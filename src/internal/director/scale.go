package director

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Scaler sets how many worker pods run.
type Scaler interface {
	Scale(ctx context.Context, replicas int) error
}

// Deployment scales the worker Deployment through its scale subresource,
// the only part of it the director's role can touch.
type Deployment struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
}

func (d Deployment) Scale(ctx context.Context, replicas int) error {
	deps := d.Client.AppsV1().Deployments(d.Namespace)
	s, err := deps.GetScale(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if s.Spec.Replicas == int32(replicas) {
		return nil
	}
	s.Spec.Replicas = int32(replicas)
	_, err = deps.UpdateScale(ctx, d.Name, s, metav1.UpdateOptions{})
	return err
}

// NoScaler is for runs with a fixed set of workers, like tests and
// `go run` outside the cluster.
type NoScaler struct{}

func (NoScaler) Scale(context.Context, int) error { return nil }
