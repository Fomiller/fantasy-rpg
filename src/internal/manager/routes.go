package manager

import (
	"context"
	"errors"
	"fmt"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var IngressRoutes = schema.GroupVersionResource{Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"}

const managedBy = "instance-manager"

// EnsureRoutes makes the GameServer's Service and IngressRoute if either is
// missing. Both are owned by the GameServer, so Kubernetes deletes them with
// it and the manager never does.
func (m *Manager) EnsureRoutes(ctx context.Context, gs *agonesv1.GameServer) error {
	err := errors.Join(m.ensureService(ctx, gs), m.ensureIngressRoute(ctx, gs))
	if err != nil {
		m.metrics.routingErrors.WithLabelValues("route").Inc()
		return fmt.Errorf("route %s: %w", gs.Name, err)
	}
	return nil
}

func (m *Manager) ensureService(ctx context.Context, gs *agonesv1.GameServer) error {
	services := m.kube.CoreV1().Services(m.cfg.Namespace)
	_, err := services.Get(ctx, gs.Name, metav1.GetOptions{})
	if !notFound(err) {
		return err
	}
	port := containerPort(gs)
	_, err = services.Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            gs.Name,
			Namespace:       m.cfg.Namespace,
			Labels:          labels(gs),
			OwnerReferences: []metav1.OwnerReference{owner(gs)},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{agonesv1.GameServerPodLabel: gs.Name},
			Ports:    []corev1.ServicePort{{Name: "ws", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (m *Manager) ensureIngressRoute(ctx context.Context, gs *agonesv1.GameServer) error {
	routes := m.dynamic.Resource(IngressRoutes).Namespace(m.cfg.Namespace)
	_, err := routes.Get(ctx, gs.Name, metav1.GetOptions{})
	if !notFound(err) {
		return err
	}
	match := "PathPrefix(`/instances/" + gs.Name + "/`)"
	if m.cfg.Host != "" {
		match = "Host(`" + m.cfg.Host + "`) && " + match
	}
	route := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "traefik.io/v1alpha1",
		"kind":       "IngressRoute",
		"spec": map[string]any{
			"entryPoints": []any{m.cfg.EntryPoint},
			"routes": []any{map[string]any{
				"match":    match,
				"kind":     "Rule",
				"services": []any{map[string]any{"name": gs.Name, "port": int64(containerPort(gs))}},
			}},
		},
	}}
	route.SetName(gs.Name)
	route.SetNamespace(m.cfg.Namespace)
	route.SetLabels(labels(gs))
	route.SetOwnerReferences([]metav1.OwnerReference{owner(gs)})
	_, err = routes.Create(ctx, route, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// owner leaves BlockOwnerDeletion off. Setting it needs update on the
// GameServer's finalizers, which the manager's role does not have.
func owner(gs *agonesv1.GameServer) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: agonesv1.SchemeGroupVersion.String(),
		Kind:       "GameServer",
		Name:       gs.Name,
		UID:        gs.UID,
		Controller: &controller,
	}
}

func labels(gs *agonesv1.GameServer) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": managedBy,
		agonesv1.GameServerPodLabel:    gs.Name,
	}
}

func containerPort(gs *agonesv1.GameServer) int32 {
	if len(gs.Spec.Ports) > 0 && gs.Spec.Ports[0].ContainerPort > 0 {
		return gs.Spec.Ports[0].ContainerPort
	}
	return 8080
}

func notFound(err error) bool {
	return apierrors.IsNotFound(err)
}
