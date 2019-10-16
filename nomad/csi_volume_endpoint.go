package nomad

import (
	"sync"
	"time"

	log "github.com/hashicorp/go-hclog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/nomad/structs"
)

type CSIVolume struct {
	srv    *Server
	logger log.Logger

	// ctx provides context regarding the underlying connection
	ctx *RPCContext

	// updates holds pending client status updates for allocations
	updates []*structs.Allocation

	// evals holds pending rescheduling eval updates triggered by failed allocations
	evals []*structs.Evaluation

	// updateFuture is used to wait for the pending batch update
	// to complete. This may be nil if no batch is pending.
	updateFuture *structs.BatchFuture

	// updateTimer is the timer that will trigger the next batch
	// update, and may be nil if there is no batch pending.
	updateTimer *time.Timer

	// updatesLock synchronizes access to the updates list,
	// the future and the timer.
	updatesLock sync.Mutex
}

// endpoint forwards the request to the leader, validates ACLs, and starts metrics. If it
// returns a non-nil function, that function should be called with delay in the endpoint
func (srv *Server) endpoint(args *structs.QueryOptions, reply structs.QueryMeta,
	forward string,
	aclCheck func(*acl.ACL) bool,
	metrics []string) (func(), error) {

	var aclObj *acl.ACL
	var err error

	// The empty function, returned if metrics are unused
	// nilFn := func() {}

	// Forward to the leader
	if done, err := srv.forward(forward, args, args, reply); done {
		return nil, err
	}

	// Enforce ACL Token
	// Lookup the token
	if aclObj, err = srv.ResolveToken(args.AuthToken); err != nil {
		// If ResolveToken had an unexpected error return that
		return nil, err
	}

	// Check the found token with the provided predicate
	if aclObj != nil && !aclCheck(aclObj) {
		return nil, structs.ErrPermissionDenied
	}

	// Fallback to treating the AuthToken as a Node.SecretID
	if aclObj == nil {
		node, stateErr := srv.fsm.State().NodeBySecretID(nil, args.AuthToken)
		if stateErr != nil {
			// Return the original ResolveToken error with this err
			var merr multierror.Error
			merr.Errors = append(merr.Errors, err, stateErr)
			return nil, merr.ErrorOrNil()
		}

		if node == nil {
			return nil, structs.ErrTokenNotFound
		}
	}

	// Start metrics
	if len(metrics) > 0 {
		start := time.Now()
		return func() {
			metrics.MeasureSince(metrics, start)
		}, nil
	}

	return nil, nil
}

func (v *CSIVolume) List(args *structs.CSIVolumeListRequest, reply structs.CSIVolumeListResponse) error {
	end, err := endpoint(args, reply, "CSIVolume.List",
		func(aclObj *acl.ACL) bool {
			return aclObj.AllowCSIVolumeRead()
		},
		[]string{"nomad", "volume", "list"})
	if err != nil {
		return err
	}
	if end != nil {
		defer end()
	}

	return nil
}

// GetVolume fetches detailed information about a specific volume
func (v *CSIVolume) GetVolume(args *structs.CSIVolumeSingleRequest, reply structs.CSIVolumeSingleResponse) error {
	end, err := endpoint(args, reply, "CSIVolume.GetVolume",
		func(aclObj *acl.ACL) bool {
			return aclObj.AllowNsOp(ns, acl.HostVolumeCapabilityMountReadOnly)
		},
		[]string{"nomad", "volume", "get"})
	if err != nil {
		return err
	}
	if end != nil {
		defer end()
	}

	return nil
}
