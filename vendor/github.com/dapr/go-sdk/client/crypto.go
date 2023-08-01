/*
Copyright 2023 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	commonv1pb "github.com/dapr/go-sdk/dapr/proto/common/v1"
	runtimev1pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

// Encrypt data read from a stream, returning a readable stream that receives the encrypted data.
// This method returns an error if the initial call fails. Errors performed during the encryption are received by the out stream.
func (c *GRPCClient) Encrypt(ctx context.Context, in io.Reader, opts EncryptOptions) (io.Reader, error) {
	// Ensure required options are present
	// This short-circuits and avoids a call to the runtime
	if opts.ComponentName == "" {
		return nil, errors.New("option 'ComponentName' is required")
	}
	if opts.KeyName == "" {
		return nil, errors.New("option 'KeyName' is required")
	}
	if opts.KeyWrapAlgorithm == "" {
		return nil, errors.New("option 'Algorithm' is required")
	}

	// Create the stream
	stream, err := c.protoClient.EncryptAlpha1(ctx)
	if err != nil {
		return nil, err
	}

	// Use the context of the stream here.
	return c.performCryptoOperation(
		stream.Context(), stream,
		in, opts,
		&runtimev1pb.EncryptRequest{},
		&runtimev1pb.EncryptResponse{},
	)
}

// Decrypt data read from a stream, returning a readable stream that receives the decrypted data.
// This method returns an error if the initial call fails. Errors performed during the encryption are received by the out stream.
func (c *GRPCClient) Decrypt(ctx context.Context, in io.Reader, opts DecryptOptions) (io.Reader, error) {
	// Ensure required options are present
	// This short-circuits and avoids a call to the runtime
	if opts.ComponentName == "" {
		return nil, errors.New("option 'ComponentName' is required")
	}

	// Create the stream
	stream, err := c.protoClient.DecryptAlpha1(ctx)
	if err != nil {
		return nil, err
	}

	// Use the context of the stream here.
	return c.performCryptoOperation(
		stream.Context(), stream,
		in, opts,
		&runtimev1pb.DecryptRequest{},
		&runtimev1pb.DecryptResponse{},
	)
}

func (c *GRPCClient) performCryptoOperation(ctx context.Context, stream grpc.ClientStream, in io.Reader, opts cryptoOperationOpts, reqProto runtimev1pb.CryptoRequests, resProto runtimev1pb.CryptoResponses) (io.Reader, error) {
	var err error
	// Pipe for writing the response
	pr, pw := io.Pipe()

	// Send the request in a background goroutine
	go func() {
		// Build the options object for the first message
		optsProto := opts.getProto()

		// Get a buffer from the pool
		reqBuf := bufPool.Get().(*[]byte)
		defer func() {
			bufPool.Put(reqBuf)
		}()

		// Send the request in chunks
		var (
			n    int
			seq  uint64
			done bool
		)
		for {
			if ctx.Err() != nil {
				pw.CloseWithError(ctx.Err())
				return
			}

			// First message only - add the options
			if optsProto != nil {
				reqProto.SetOptions(optsProto)
				optsProto = nil
			} else {
				// Reset the object so we can re-use it
				reqProto.Reset()
			}

			n, err = in.Read(*reqBuf)
			if err == io.EOF {
				done = true
			} else if err != nil {
				pw.CloseWithError(err)
				return
			}

			// Send the chunk if there's anything to send
			if n > 0 {
				reqProto.SetPayload(&commonv1pb.StreamPayload{
					Data: (*reqBuf)[:n],
					Seq:  seq,
				})
				seq++

				err = stream.SendMsg(reqProto)
				if errors.Is(err, io.EOF) {
					// If SendMsg returns an io.EOF error, it usually means that there's a transport-level error
					// The exact error can only be determined by RecvMsg, so if we encounter an EOF error here, just consider the stream done and let RecvMsg handle the error
					done = true
				} else if err != nil {
					pw.CloseWithError(fmt.Errorf("error sending message: %w", err))
					return
				}
			}

			// Stop the loop with the last chunk
			if done {
				err = stream.CloseSend()
				if err != nil {
					pw.CloseWithError(fmt.Errorf("failed to close the send direction of the stream: %w", err))
					return
				}

				break
			}
		}
	}()

	// Read the response in another goroutine
	go func() {
		var (
			expectSeq uint64
			readErr   error
			done      bool
			payload   *commonv1pb.StreamPayload
		)

		// Read until the end of the stream
		for {
			if ctx.Err() != nil {
				pw.CloseWithError(ctx.Err())
				return
			}

			// Read the next chunk
			readErr = stream.RecvMsg(resProto)
			if errors.Is(readErr, io.EOF) {
				// Receiving an io.EOF signifies that the client has stopped sending data over the pipe, so this is the end
				done = true
			} else if readErr != nil {
				pw.CloseWithError(fmt.Errorf("error receiving message: %w", readErr))
				return
			}

			// Write the data, if any, into the pipe
			payload = resProto.GetPayload()
			if payload != nil {
				if payload.Seq != expectSeq {
					pw.CloseWithError(fmt.Errorf("invalid sequence number in chunk: %d (expected: %d)", payload.Seq, expectSeq))
					return
				}
				expectSeq++

				_, readErr = pw.Write(payload.Data)
				if readErr != nil {
					pw.CloseWithError(fmt.Errorf("error writing data: %w", readErr))
					return
				}
			}

			// Stop when done
			if done {
				break
			}

			// Reset the proto
			resProto.Reset()
		}

		// Close the writer of the pipe when done
		pw.Close()
	}()

	// Return the readable stream
	return pr, nil
}

// Interface for EncryptOptions and DecryptOptions
type cryptoOperationOpts interface {
	getProto() proto.Message
}

// EncryptOptions contains options passed to the Encrypt method.
type EncryptOptions struct {
	// Name of the component. Required.
	ComponentName string
	// Name (or name/version) of the key. Required.
	KeyName string
	// Key wrapping algorithm to use. Required.
	// Supported options include: A256KW, A128CBC, A192CBC, A256CBC, RSA-OAEP-256.
	KeyWrapAlgorithm string
	// DataEncryptionCipher to use to encrypt data (optional): "aes-gcm" (default) or "chacha20-poly1305"
	DataEncryptionCipher string
	// If true, the encrypted document does not contain a key reference.
	// In that case, calls to the Decrypt method must provide a key reference (name or name/version).
	// Defaults to false.
	OmitDecryptionKeyName bool
	// Key reference to embed in the encrypted document (name or name/version).
	// This is helpful if the reference of the key used to decrypt the document is different from the one used to encrypt it.
	// If unset, uses the reference of the key used to encrypt the document (this is the default behavior).
	// This option is ignored if omit_decryption_key_name is true.
	DecryptionKeyName string
}

func (o EncryptOptions) getProto() proto.Message {
	return &runtimev1pb.EncryptRequestOptions{
		ComponentName:         o.ComponentName,
		KeyName:               o.KeyName,
		KeyWrapAlgorithm:      o.KeyWrapAlgorithm,
		DataEncryptionCipher:  o.DataEncryptionCipher,
		OmitDecryptionKeyName: o.OmitDecryptionKeyName,
		DecryptionKeyName:     o.DecryptionKeyName,
	}
}

// DecryptOptions contains options passed to the Decrypt method.
type DecryptOptions struct {
	// Name of the component. Required.
	ComponentName string
	// Name (or name/version) of the key to decrypt the message.
	// Overrides any key reference included in the message if present.
	// This is required if the message doesn't include a key reference (i.e. was created with omit_decryption_key_name set to true).
	KeyName string
}

func (o DecryptOptions) getProto() proto.Message {
	return &runtimev1pb.DecryptRequestOptions{
		ComponentName: o.ComponentName,
		KeyName:       o.KeyName,
	}
}
