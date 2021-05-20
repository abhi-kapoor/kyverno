package leaderelection

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)


type Interface interface {

	// Run runs a leader election
	Run(ctx context.Context)

	// ID returns this instances unique identifier
	ID() string

	// Name returns the name of the leader election
	Name() string

	// Namespace is the Kubernetes namespace used to coordinate the leader election
	Namespace() string

	// IsLeader indicates if this instance is the leader
	IsLeader() bool
}

type leaderElection struct {
	name       string
	namespace  string
	id         string
	startWork  func()
	stopWork   func()
	kubeClient kubernetes.Interface
	lock       resourcelock.Interface
	isLeader   int64
	log        logr.Logger
}

func New(name, namespace string, kubeClient kubernetes.Interface, startWork, stopWork func(), log logr.Logger) (Interface, error) {
	id, err := os.Hostname()
	if err != nil {
		return nil, errors.Wrap(err, "error fetching hostname")
	}

	id = id + "_" + string(uuid.NewUUID())

	lock, err := resourcelock.New(
		resourcelock.ConfigMapsResourceLock,
		namespace,
		name,
		kubeClient.CoreV1(),
		kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: id,
		},
	)

	if err != nil {
		return nil, errors.Wrapf(err, "error creating lock for leader election %s in namespace %s", namespace, name)
	}

	return &leaderElection{
		name:       name,
		namespace:  namespace,
		kubeClient: kubeClient,
		lock:       lock,
		startWork:  startWork,
		stopWork:   stopWork,
		log:        log,
	}, nil
}

func (le *leaderElection) Name() string {
	return le.name
}

func (le *leaderElection) Namespace() string {
	return le.namespace
}

func (le *leaderElection) IsLeader() bool {
	return atomic.LoadInt64(&le.isLeader) == 1
}

func (le *leaderElection) ID() string {
	return le.lock.Identity()
}

func (le *leaderElection) Run(ctx context.Context) {

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            le.lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				atomic.StoreInt64(&le.isLeader, 1)
				le.log.WithValues("id", le.lock.Identity()).Info("started leading")
				if le.startWork != nil {
					go le.startWork()
				}
			},

			OnStoppedLeading: func() {
				atomic.StoreInt64(&le.isLeader, 0)
				le.log.WithValues("id", le.lock.Identity()).Info("stopped leading")
				if le.stopWork != nil {
					go le.stopWork()
				}
			},

			OnNewLeader: func(identity string) {
				if identity == le.lock.Identity() {
					return
				}
				le.log.WithValues("current id", le.lock.Identity(), "leader", identity).Info("another instance has been elected as leader")
			},
		},
	})
}