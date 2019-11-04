package webhook

import (
	"strconv"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReportRequest(t *testing.T) {
	admissionRequest := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
		Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "Pods"},
		Name:      "admissionRequestTest",
		Namespace: "default",
	}

	admissionResponse := &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}

	expectedTags := map[string]string{
		requestOperationKey.Name():  string(admissionRequest.Operation),
		kindGroupKey.Name():         admissionRequest.Kind.Group,
		kindVersionKey.Name():       admissionRequest.Kind.Version,
		kindKindKey.Name():          admissionRequest.Kind.Kind,
		resourceGroupKey.Name():     admissionRequest.Resource.Group,
		resourceVersionKey.Name():   admissionRequest.Resource.Version,
		resourceResourceKey.Name():  admissionRequest.Resource.Resource,
		resourceNameKey.Name():      admissionRequest.Name,
		resourceNamespaceKey.Name(): admissionRequest.Namespace,
		admissionAllowedKey.Name():  strconv.FormatBool(admissionResponse.Allowed),
	}

	expectedLatencyValueMin := time.Duration(100 * time.Millisecond)
	expectedLatencyValueMax := time.Duration(500 * time.Millisecond)
	var expectedLatencyMin float64 = 100
	var expectedLatencyMax float64 = 500
	var expectedCount int64 = 2

	r, err := NewStatsReporter()
	if err != nil {
		t.Errorf("NewStatsReporter() error %v", err)
	}

	err = r.ReportRequest(admissionRequest, admissionResponse, expectedLatencyValueMin)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}
	err = r.ReportRequest(admissionRequest, admissionResponse, expectedLatencyValueMax)
	if err != nil {
		t.Errorf("ReportRequest error %v", err)
	}

	// count test
	row, err := view.RetrieveData(requestCountName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, requestCountName)
	}
	count, ok := row[0].Data.(*view.CountData)
	if !ok {
		t.Error("ReportRequest should have aggregation Count()")
	}
	for _, tag := range row[0].Tags {
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportRequest tags does not match for %v", tag.Key.Name())
		}
	}
	if count.Value != expectedCount {
		t.Errorf("Metric: %v - Expected %v, got %v. ", requestCountName, count.Value, expectedCount)
	}

	// latency test
	row, err = view.RetrieveData(requestLatenciesName)
	if err != nil {
		t.Errorf("Error when retrieving data: %v from %v", err, requestLatenciesName)
	}
	latencyValue, ok := row[0].Data.(*view.DistributionData)
	if !ok {
		t.Error("ReportRequest should have aggregation Distribution()")
	}
	for _, tag := range row[0].Tags {
		if tag.Value != expectedTags[tag.Key.Name()] {
			t.Errorf("ReportRequest tags does not match for %v", tag.Key.Name())
		}
	}
	if latencyValue.Min != expectedLatencyMin {
		t.Errorf("Metric: %v - Expected %v, got %v. ", requestLatenciesName, latencyValue.Min, expectedLatencyMin)
	}
	if latencyValue.Max != expectedLatencyMax {
		t.Errorf("Metric: %v - Expected %v, got %v. ", requestLatenciesName, latencyValue.Max, expectedLatencyMax)
	}
}
