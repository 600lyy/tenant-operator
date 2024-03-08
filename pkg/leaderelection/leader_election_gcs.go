package leaderelection

import (
	"fmt"

	"github.com/google/uuid"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Options provides the required configuration to create a new lease lock.
type Option struct {
	// LeaderElection determines whether or not to use leader election when
	// starting the manager.
	LeaderElection bool

	// LeaderElectionID determines the name of the resource that leader election
	// will use for holding the leader lock.
	LeaderElectionID string

	// Google project that holds the storage bucket
	ProjectID string

	// storage bucket name
	Bucket string

	// Lease file anme
	Lease string
}

// NewResourceLock creates a new lease lock for use in a leader election loop.
func NewLeaseLock(options Option, clientId string) (resourcelock.Interface, error) {
	if !options.LeaderElection {
		return nil, nil
	}
	if clientId == "" {
		return nil, fmt.Errorf("unable to create a lock as client Id is empty")
	}

	/* 	if client, err = storage.NewClient(ctx); err != nil {
	   		return nil, err
	   	}
	*/
	id := clientId + "_" + (uuid.New().String())

	return &LeaseLock{
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
		ProjectId:  options.ProjectID,
		BucketName: options.Bucket,
		LeaseFile:  options.Lease,
	}, nil
}
