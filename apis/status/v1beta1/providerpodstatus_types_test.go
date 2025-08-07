/*

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

package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestNewProviderStatusForPod(t *testing.T) {
	scheme := runtime.NewScheme()
	
	// Register corev1 types
	err := corev1.AddToScheme(scheme)
	assert.NoError(t, err)
	
	// Set up environment variable for namespace
	t.Setenv("POD_NAMESPACE", "test-namespace")
	
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
			UID:       types.UID("pod-uid"),
		},
	}

	providerName := "test-provider"
	providerUID := types.UID("test-uid")

	status, createErr := NewProviderStatusForPod(pod, providerName, providerUID, scheme)
	assert.NoError(t, createErr)
	assert.NotNil(t, status)

	assert.Equal(t, KeyForProvider(pod.Name, providerName), status.Name)
	assert.Equal(t, pod.Name, status.Status.ID)
	assert.Equal(t, providerUID, status.Status.ProviderUID)
}

func TestKeyForProvider(t *testing.T) {
	podName := "test-pod"
	providerName := "test-provider"
	
	key := KeyForProvider(podName, providerName)
	expected := "test-pod-test-provider"
	
	assert.Equal(t, expected, key)
}