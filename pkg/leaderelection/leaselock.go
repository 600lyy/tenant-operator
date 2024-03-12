package leaderelection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"cloud.google.com/go/storage"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

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
	var recordByte []byte
	var rc *storage.Reader
	var gcsErr error
	var record *resourcelock.LeaderElectionRecord

	if rc, gcsErr = ll.Client.Bucket(ll.BucketName).Object(ll.LeaseFile).NewReader(ctx); gcsErr != nil {
		return nil, nil, ll.convertGcsErrToLeaseErr(gcsErr)
	}
	defer rc.Close()

	if _, gcsErr = rc.Read(recordByte); gcsErr != nil {
		return nil, nil, ll.convertGcsErrToLeaseErr(gcsErr)
	}

	if gcsErr = json.Unmarshal(recordByte, record); gcsErr != nil {
		return nil, nil, ll.convertGcsErrToLeaseErr(gcsErr)
	}
	return record, recordByte, nil
}

// Create attempts to create a lease
func (ll *LeaseLock) Create(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
	var bkt *storage.BucketHandle
	var attr *storage.BucketAttrs
	var err error
	var len int
	bkt = ll.Client.Bucket(ll.BucketName)
	attr, err = bkt.Attrs(ctx)
	if err != nil {
		return err
	}
	klog.V(5).Info("Succesfully connected to the bucket [%v] in Google storage", attr.Name)

	wc := bkt.Object(ll.LeaseFile).NewWriter(ctx)
	writeByte, err := json.Marshal(ler)
	if err != nil {
		return err
	}
	if len, err = wc.Write(writeByte); err != nil {
		return err
	}
	klog.V(5).Info("Succesfully write [%d] bytes to the lease [%v/%v] in Google storage", len, attr.Name, wc.ObjectAttrs.Name)
	return nil
}

// Update will update an exising lease
// For GCS, leader needs to update the time stamps in the lease file
func (ll *LeaseLock) Update(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
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
	klog.V(5).Info("Starting up the client to talk with google cloud storage")
	var client *storage.Client
	var err error
	if client, err = storage.NewClient(ctx); err != nil {
		return err
	}
	ll.Client = client
	return nil
}

func (ll *LeaseLock) convertGcsErrToLeaseErr(gcsErr error) *errors.StatusError {
	return &errors.StatusError{
		ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    http.StatusNotFound,
			Reason:  metav1.StatusReasonNotFound,
			Message: gcsErr.Error(),
		},
	}
}
