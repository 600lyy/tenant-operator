// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package leaderelection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"cloud.google.com/go/storage"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
)

var logger = klog.Log

// LeaseLock implements the resourcelock.Interface
// in the controller manager
type LeaseLock struct {
	*storage.Client
	LockConfig resourcelock.ResourceLockConfig
	ProjectId  string
	BucketName string
	LeaseFile  string
}

// Get returns the lease object from the storage bucket
func (ll *LeaseLock) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	var rc *storage.Reader
	var gcsErr error
	var len int
	var recordByte = make([]byte, 200)
	var record = resourcelock.LeaderElectionRecord{}

	if ll.Client == nil {
		return nil, nil, errors.New("storage client is not empty, initiate it first")
	}

	if rc, gcsErr = ll.Client.Bucket(ll.BucketName).Object(ll.LeaseFile).NewReader(ctx); gcsErr != nil {
		return nil, nil, ll.convertGcsErrToLeaseErr(gcsErr)
	}
	defer rc.Close()

	if len, gcsErr = rc.Read(recordByte); gcsErr != nil {
		return nil, nil, ll.convertGcsErrToLeaseErr(gcsErr)
	}

	if gcsErr = json.Unmarshal(recordByte[:len], &record); gcsErr != nil {
		return nil, nil, ll.convertGcsErrToLeaseErr(gcsErr)
	}
	logger.Info("reading lease content from GCS", "manager-identify", ll.LockConfig.Identity, "bytes", len, "bucket", ll.BucketName, "lease", ll.LeaseFile)
	return &record, recordByte, nil
}

// Create attempts to create a lease
func (ll *LeaseLock) Create(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	var bkt *storage.BucketHandle
	var attr *storage.BucketAttrs
	var err error
	var len int

	if ll.Client == nil {
		return errors.New("storage client is not empty, initiate it first")
	}

	bkt = ll.Client.Bucket(ll.BucketName)
	attr, err = bkt.Attrs(ctx)
	if err != nil {
		return err
	}
	logger.Info("succesfully connected to Google storage", "bucket", attr.Name)

	wc := bkt.Object(ll.LeaseFile).NewWriter(ctx)
	defer wc.Close()

	writeByte, err := json.Marshal(ler)
	if err != nil {
		return err
	}
	if len, err = wc.Write(writeByte); err != nil {
		return err
	}

	logger.Info("createing lease in Google storage", "bytes", len, "bucket", attr.Name, "lease", wc.ObjectAttrs.Name)
	return nil
}

// Update will update an exising lease
// For GCS, leader needs to update the time stamps in the lease file
func (ll *LeaseLock) Update(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	var err error
	var len int

	if ll.Client == nil {
		return errors.New("storage client is not empty, initiate it first")
	}
	wc := ll.Client.Bucket(ll.BucketName).Object(ll.LeaseFile).NewWriter(ctx)
	writeByte, err := json.Marshal(ler)
	if err != nil {
		return err
	}
	if len, err = wc.Write(writeByte); err != nil {
		return err
	}
	if err = wc.Close(); err != nil {
		return err
	}
	logger.Info("updating lease in Google storage", "bytes", len, "bucket", ll.BucketName, "lease", wc.ObjectAttrs.Name)
	return nil
}

// RecordEvent is used to record events
func (ll *LeaseLock) RecordEvent(string) {
	// TODO
}

// Identity will return the locks Identity
func (ll *LeaseLock) Identity() string {
	return ll.LockConfig.Identity
}

// Describe is used to convert details on current resource lock
// into a string
func (ll *LeaseLock) Describe() string {
	return fmt.Sprintf("%v/%v", ll.BucketName, ll.LeaseFile)
}

func (ll *LeaseLock) StartGCSClient(ctx context.Context) error {
	logger.Info("starting up the client to talk with google cloud storage")
	var client *storage.Client
	var err error
	if client, err = storage.NewClient(ctx); err != nil {
		return err
	}
	ll.Client = client
	return nil
}

func (ll *LeaseLock) convertGcsErrToLeaseErr(gcsErr error) *apierrors.StatusError {
	var statusReason metav1.StatusReason
	var statusErr *apierrors.StatusError

	if errors.Is(gcsErr, storage.ErrObjectNotExist) {
		statusReason = metav1.StatusReasonNotFound
	} else {
		statusReason = metav1.StatusReasonInternalError
	}
	statusErr = &apierrors.StatusError{
		ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    http.StatusNotFound,
			Reason:  statusReason,
			Message: gcsErr.Error(),
		},
	}
	return statusErr
}
