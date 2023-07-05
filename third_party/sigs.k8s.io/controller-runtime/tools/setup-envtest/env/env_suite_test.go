/*
Copyright 2021 The Kubernetes Authors.

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

package env_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var testLog logr.Logger

func zapLogger() logr.Logger {
	testOut := zapcore.AddSync(GinkgoWriter)
	enc := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	// bleh setting up logging to the ginkgo writer is annoying
	zapLog := zap.New(zapcore.NewCore(enc, testOut, zap.DebugLevel),
		zap.ErrorOutput(testOut), zap.Development(), zap.AddStacktrace(zap.WarnLevel))
	return zapr.NewLogger(zapLog)
}

func TestEnv(t *testing.T) {
	testLog = zapLogger()

	RegisterFailHandler(Fail)
	RunSpecs(t, "Env Suite")
}
