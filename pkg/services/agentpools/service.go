package agentpools

import (
	"context"
	"time"

	"github.com/Azure/go-autorest/autorest"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/alexeldeib/azsvc/api/v1alpha1"
	"github.com/alexeldeib/azsvc/pkg/errors"
)

const (
	backoffSteps    = 30
	backoffFactor   = 1.25
	backoffInterval = 5 * time.Second
	backoffJitter   = 1
	backoffLimit    = 2700 * time.Second
)

func backoff() wait.Backoff {
	return wait.Backoff{
		Cap:      backoffLimit,
		Steps:    backoffSteps,
		Factor:   backoffFactor,
		Duration: backoffInterval,
		Jitter:   backoffJitter,
	}
}

type Service struct {
	authorizer autorest.Authorizer
	log        logr.Logger
}

func NewService(authorizer autorest.Authorizer, log logr.Logger) *Service {
	return &Service{
		authorizer,
		log,
	}
}

func (s *Service) Ensure(ctx context.Context, obj *v1alpha1.AgentPool) error {
	client, err := newClient(s.authorizer, obj.Spec.SubscriptionID)
	if err != nil {
		return err
	}
	spec, err := s.Get(ctx, obj.Spec.SubscriptionID, obj.Spec.ResourceGroup, obj.Spec.Cluster, obj.Spec.Name)
	if err != nil {
		return err
	}

	spec.Set(
		Name(obj.Spec.Name),
		SKU(obj.Spec.SKU),
		Cluster(obj.Spec.Cluster),
		SubscriptionID(obj.Spec.SubscriptionID),
		ResourceGroup(obj.Spec.ResourceGroup),
		KubernetesVersion(obj.Spec.Version),
	)

	s.log.Info("beginning long create/update operation")
	future, err := client.CreateOrUpdate(ctx, obj.Spec.ResourceGroup, obj.Spec.Cluster, obj.Spec.Name, spec.internal)
	if err != nil {
		return err
	}

	return wait.ExponentialBackoff(backoff(), func() (done bool, err error) {
		s.log.Info("reconciling with backoff")
		done, err = future.DoneWithContext(ctx, client)
		if err != nil {
			s.log.Error(err, "failed reconcile attempt")
		}
		return done && err == nil, nil
	})
}

func (s *Service) Delete(ctx context.Context, obj *v1alpha1.AgentPool) error {
	client, err := newClient(s.authorizer, obj.Spec.SubscriptionID)
	if err != nil {
		return err
	}
	err = client.delete(ctx, obj.Spec.ResourceGroup, obj.Spec.Name, obj.Spec.Cluster)
	if err != nil && errors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *Service) Get(ctx context.Context, subscriptionID, resourceGroup, cluster, name string) (*Spec, error) {
	client, err := newClient(s.authorizer, subscriptionID)
	if err != nil {
		return nil, err
	}

	result, err := client.get(ctx, resourceGroup, cluster, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return defaultSpec(), nil
		}
		return nil, err
	}

	return &Spec{
		internal: result,
	}, nil
}
