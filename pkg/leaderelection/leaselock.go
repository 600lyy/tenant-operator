package leaderelection

import (
	"context"
	"encoding/json"
	"time"

	"cloud.google.com/go/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
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
	// For testing, fake a lease record without fetching the object from the bucket
	record := &resourcelock.LeaderElectionRecord{
		HolderIdentity:       ll.Identity(),
		LeaseDurationSeconds: 1000.,
		AcquireTime:          metav1.Time{Time: time.Now()},
		RenewTime:            metav1.Time{Time: time.Now()},
		LeaderTransitions:    1,
	}
	recordByte, err := json.Marshal(*record)
	if err != nil {
		return nil, nil, err
	}

	return record, recordByte, nil
}

// Create attempts to create a lease
// For GCS, we never create a lease from the manager. The lease is created externally
func (ll *LeaseLock) Create(ctx context.Context, ler resourcelock.LeaderElectionRecord) error {
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
	return ""
}
