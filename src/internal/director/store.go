package director

import (
	"context"
	"strconv"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Store keeps the target across director restarts.
type Store interface {
	// Load says false when nothing was ever saved.
	Load(ctx context.Context) (Target, bool, error)
	Save(ctx context.Context, t Target) error
}

// ConfigMap keeps the target in a ConfigMap the director makes on its first
// save. The chart does not render it, so Argo CD never reverts a change.
type ConfigMap struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
}

func (c ConfigMap) Load(ctx context.Context) (Target, bool, error) {
	cm, err := c.Client.CoreV1().ConfigMaps(c.Namespace).Get(ctx, c.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Target{}, false, nil
	}
	if err != nil {
		return Target{}, false, err
	}
	n, err := strconv.Atoi(cm.Data["count"])
	if err != nil {
		return Target{}, false, err
	}
	t := Target{Mode: Mode(cm.Data["mode"]), Count: n}
	return t, true, t.Validate()
}

func (c ConfigMap) Save(ctx context.Context, t Target) error {
	maps := c.Client.CoreV1().ConfigMaps(c.Namespace)
	data := map[string]string{"mode": string(t.Mode), "count": strconv.Itoa(t.Count)}
	cm, err := maps.Get(ctx, c.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = maps.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.Name,
				Namespace: c.Namespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "bot-director"},
			},
			Data: data,
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	cm.Data = data
	_, err = maps.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// Memory is a Store for tests and runs outside the cluster.
type Memory struct {
	mu sync.Mutex
	t  *Target
}

func (m *Memory) Load(context.Context) (Target, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t == nil {
		return Target{}, false, nil
	}
	return *m.t, true, nil
}

func (m *Memory) Save(_ context.Context, t Target) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = &t
	return nil
}
