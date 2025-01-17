/*
Copyright 2019 The Jetstack cert-manager contributors.

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

package util

import (
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clocktesting "k8s.io/utils/clock/testing"

	apiutil "github.com/jetstack/cert-manager/pkg/api/util"
	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	controllertest "github.com/jetstack/cert-manager/pkg/controller/test"
	"github.com/jetstack/cert-manager/pkg/util"
	"github.com/jetstack/cert-manager/test/unit/gen"
)

var (
	fixedClockStart = time.Now()
	fixedClock      = clocktesting.NewFakeClock(fixedClockStart)
)

type reporterT struct {
	certificateRequest *cmapi.CertificateRequest

	err             error
	message, reason string

	runFailedNotPending bool

	expectedEvents      []string
	expectedConditions  []cmapi.CertificateRequestCondition
	expectedFailureTime *metav1.Time
}

func TestReporter(t *testing.T) {
	nowMetaTime := metav1.NewTime(fixedClockStart)
	oldMetaTime := metav1.NewTime(time.Time{})

	baseCR := gen.CertificateRequest("test")

	exampleErr := errors.New("this is an error")
	exampleMessage := "this is a message"
	exampleReason := "ThisIsAReason"

	failedCondition := cmapi.CertificateRequestCondition{
		Type:               cmapi.CertificateRequestConditionReady,
		Reason:             "Failed",
		Message:            exampleMessage + ": " + exampleErr.Error(),
		Status:             "False",
		LastTransitionTime: &nowMetaTime,
	}

	pendingCondition := cmapi.CertificateRequestCondition{
		Type:               cmapi.CertificateRequestConditionReady,
		Reason:             "Pending",
		Message:            exampleMessage + ": " + exampleErr.Error(),
		Status:             "False",
		LastTransitionTime: &nowMetaTime,
	}

	existingPendingCondition := cmapi.CertificateRequestCondition{
		Type:               cmapi.CertificateRequestConditionReady,
		Reason:             "Pending",
		Message:            "Exisitng Pending Message",
		Status:             "False",
		LastTransitionTime: &nowMetaTime,
	}

	tests := map[string]reporterT{
		"a failed report should update the conditions and set FailureTime as it is nil": {
			certificateRequest: gen.CertificateRequestFrom(baseCR),
			err:                exampleErr,
			message:            exampleMessage,
			reason:             exampleReason,

			expectedEvents: []string{
				"Warning ThisIsAReason this is a message: this is an error",
			},
			expectedConditions:  []cmapi.CertificateRequestCondition{failedCondition},
			expectedFailureTime: &nowMetaTime,

			runFailedNotPending: true,
		},

		"a failed report should update the conditions and not FailureTime as it is not nil": {
			certificateRequest: gen.CertificateRequestFrom(baseCR,
				gen.SetCertificateRequestFailureTime(oldMetaTime),
			),
			err:     exampleErr,
			message: exampleMessage,
			reason:  exampleReason,

			expectedEvents: []string{
				"Warning ThisIsAReason this is a message: this is an error",
			},
			expectedConditions:  []cmapi.CertificateRequestCondition{failedCondition},
			expectedFailureTime: &oldMetaTime,

			runFailedNotPending: true,
		},

		"a pending report should update the conditions and send an event as a Pending condition already exists": {
			certificateRequest: gen.CertificateRequestFrom(baseCR),
			err:                exampleErr,
			message:            exampleMessage,
			reason:             exampleReason,

			expectedEvents: []string{
				"Normal ThisIsAReason this is a message: this is an error",
			},
			expectedConditions:  []cmapi.CertificateRequestCondition{pendingCondition},
			expectedFailureTime: nil,

			runFailedNotPending: false,
		},

		"a pending report should update the conditions and not send an event as a Pending condition already exists": {
			certificateRequest: gen.CertificateRequestFrom(baseCR,
				gen.SetCertificateRequestStatusCondition(existingPendingCondition),
			),
			err:     exampleErr,
			message: exampleMessage,
			reason:  exampleReason,

			// No event sent
			expectedEvents:      []string{},
			expectedConditions:  []cmapi.CertificateRequestCondition{pendingCondition},
			expectedFailureTime: nil,

			runFailedNotPending: false,
		},
		"a pending report should update the conditions and send an event as only a non Pending condition already exists": {
			certificateRequest: gen.CertificateRequestFrom(baseCR,
				gen.SetCertificateRequestStatusCondition(failedCondition),
			),
			err:     exampleErr,
			message: exampleMessage,
			reason:  exampleReason,

			// No event sent
			expectedEvents: []string{
				"Normal ThisIsAReason this is a message: this is an error",
			},
			expectedConditions:  []cmapi.CertificateRequestCondition{pendingCondition},
			expectedFailureTime: nil,

			runFailedNotPending: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fixedClock.SetTime(fixedClockStart)
			apiutil.Clock = fixedClock
			test.runTest(t)
		})
	}
}

func (tt *reporterT) runTest(t *testing.T) {
	recorder := new(controllertest.FakeRecorder)
	reporter := NewReporter(fixedClock, recorder)

	if tt.runFailedNotPending {
		reporter.Failed(tt.certificateRequest, tt.err,
			tt.reason, tt.message)
	} else {
		reporter.Pending(tt.certificateRequest, tt.err,
			tt.reason, tt.message)
	}

	expConditions := conditionsToString(tt.expectedConditions)
	gotConditions := conditionsToString(tt.certificateRequest.Status.Conditions)
	if expConditions != gotConditions {
		t.Errorf("got unexpected conditions response exp=%+v got=%+v",
			expConditions, gotConditions)
	}

	if !util.EqualSorted(tt.expectedEvents, recorder.Events) {
		t.Errorf("got unexpected events, exp=%+v got=%+v",
			tt.expectedEvents, recorder.Events)
	}

	if tt.expectedFailureTime == nil {
		if tt.certificateRequest.Status.FailureTime != nil {
			t.Errorf("got unexpected failure time, exp=nil got=%+v",
				tt.certificateRequest.Status.FailureTime)
		}

	} else {
		if tt.certificateRequest.Status.FailureTime.String() !=
			tt.expectedFailureTime.String() {
			t.Errorf("got unexpected failure time, exp=%+v got=%+v",
				tt.expectedFailureTime.String(),
				tt.certificateRequest.Status.FailureTime.String())
		}
	}
}

func conditionsToString(conds []cmapi.CertificateRequestCondition) string {
	return fmt.Sprintf("%+v", conds)
}
