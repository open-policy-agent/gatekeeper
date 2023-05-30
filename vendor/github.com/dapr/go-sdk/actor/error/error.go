/*
Copyright 2021 The Dapr Authors
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

package error

type ActorErr uint8

// TODO(@laurence) the classification, handle and print log of error should be optimized.
const (
	Success                       = ActorErr(0)
	ErrActorTypeNotFound          = ActorErr(1)
	ErrRemindersParamsInvalid     = ActorErr(2)
	ErrActorMethodNoFound         = ActorErr(3)
	ErrActorInvokeFailed          = ActorErr(4)
	ErrReminderFuncUndefined      = ActorErr(5)
	ErrActorMethodSerializeFailed = ActorErr(6)
	ErrActorSerializeNoFound      = ActorErr(7)
	ErrActorIDNotFound            = ActorErr(8)
	ErrActorFactoryNotSet         = ActorErr(9)
	ErrTimerParamsInvalid         = ActorErr(10)
	ErrSaveStateFailed            = ActorErr(11)
	ErrActorServerInvalid         = ActorErr(12)
)
