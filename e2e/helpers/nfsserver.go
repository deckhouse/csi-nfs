/*
Copyright 2025 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helpers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NFSServerConfig represents configuration for an NFS server deployment
type NFSServerConfig struct {
	Name       string // Deployment/Service name
	Namespace  string // Namespace to deploy to
	NFSVersion string // NFS version: "3", "4.1", or "4.2"
	SharePath  string // NFS share path (default: /exports)
}

// NFSServerInfo contains information about a deployed NFS server
type NFSServerInfo struct {
	Name       string
	Namespace  string
	NFSVersion string
	ServiceIP  string
	SharePath  string
}

// NFSServerManager manages NFS server deployments for testing
type NFSServerManager struct {
	client *kubernetes.Clientset
}

// NewNFSServerManager creates a new NFS server manager
func NewNFSServerManager(config *rest.Config) (*NFSServerManager, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return &NFSServerManager{client: clientset}, nil
}

// DeployNFSServer deploys an NFS server with the specified version
func (m *NFSServerManager) DeployNFSServer(ctx context.Context, cfg NFSServerConfig) (*NFSServerInfo, error) {
	if cfg.SharePath == "" {
		cfg.SharePath = "/exports"
	}

	// Create namespace if it doesn't exist
	if err := m.ensureNamespace(ctx, cfg.Namespace); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace: %w", err)
	}

	// Create PVC for NFS data
	if err := m.createNFSPVC(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to create NFS PVC: %w", err)
	}

	// Create NFS server deployment
	if err := m.createNFSDeployment(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to create NFS deployment: %w", err)
	}

	// Create NFS service
	if err := m.createNFSService(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to create NFS service: %w", err)
	}

	// Wait for deployment to be ready
	if err := m.waitForDeploymentReady(ctx, cfg.Namespace, cfg.Name, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("failed waiting for NFS deployment to be ready: %w", err)
	}

	// Get service IP
	serviceIP, err := m.getServiceClusterIP(ctx, cfg.Namespace, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get NFS service IP: %w", err)
	}

	return &NFSServerInfo{
		Name:       cfg.Name,
		Namespace:  cfg.Namespace,
		NFSVersion: cfg.NFSVersion,
		ServiceIP:  serviceIP,
		SharePath:  cfg.SharePath,
	}, nil
}

func (m *NFSServerManager) ensureNamespace(ctx context.Context, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err := m.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (m *NFSServerManager) createNFSPVC(ctx context.Context, cfg NFSServerConfig) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name + "-data",
			Namespace: cfg.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	_, err := m.client.CoreV1().PersistentVolumeClaims(cfg.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (m *NFSServerManager) createNFSDeployment(ctx context.Context, cfg NFSServerConfig) error {
	replicas := int32(1)

	// Use erichough/nfs-server which has better support for different NFS versions
	// Environment variables control which versions are enabled
	env := []corev1.EnvVar{
		// Export path configuration - format: "share_name:path:options"
		{Name: "NFS_EXPORT_0", Value: fmt.Sprintf("%s *(rw,sync,no_subtree_check,no_root_squash,fsid=0)", cfg.SharePath)},
	}

	// Add version-specific environment variables for erichough/nfs-server
	// See: https://github.com/ehough/docker-nfs-server
	switch cfg.NFSVersion {
	case "3":
		// For NFSv3, enable v3 and set static ports for mountd/statd/lockd
		env = append(env,
			corev1.EnvVar{Name: "NFS_DISABLE_VERSION_3", Value: "0"},
			// Static ports for NFSv3 (required for firewall/service discovery)
			corev1.EnvVar{Name: "NFS_PORT_MOUNTD", Value: "20048"},
			corev1.EnvVar{Name: "NFS_PORT_STATD_IN", Value: "32765"},
			corev1.EnvVar{Name: "NFS_PORT_STATD_OUT", Value: "32766"},
			corev1.EnvVar{Name: "NFS_PORT_NLOCKMGR", Value: "32767"},
		)
	case "4.1", "4.2":
		// For NFSv4.x, disable v3 (only NFSv4 will be available)
		env = append(env,
			corev1.EnvVar{Name: "NFS_DISABLE_VERSION_3", Value: "1"},
		)
	}

	privileged := true
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
			Labels: map[string]string{
				"app":         "nfs-server",
				"nfs-version": cfg.NFSVersion,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":         "nfs-server",
					"nfs-version": cfg.NFSVersion,
					"instance":    cfg.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":         "nfs-server",
						"nfs-version": cfg.NFSVersion,
						"instance":    cfg.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nfs-server",
							Image: "erichough/nfs-server:2.2.1",
							Env:   env,
							Ports: []corev1.ContainerPort{
								{Name: "nfs", ContainerPort: 2049, Protocol: corev1.ProtocolTCP},
								{Name: "nfs-udp", ContainerPort: 2049, Protocol: corev1.ProtocolUDP},
								{Name: "mountd", ContainerPort: 20048, Protocol: corev1.ProtocolTCP},
								{Name: "mountd-udp", ContainerPort: 20048, Protocol: corev1.ProtocolUDP},
								{Name: "rpcbind", ContainerPort: 111, Protocol: corev1.ProtocolTCP},
								{Name: "rpcbind-udp", ContainerPort: 111, Protocol: corev1.ProtocolUDP},
								{Name: "statd", ContainerPort: 32765, Protocol: corev1.ProtocolTCP},
								{Name: "statd-udp", ContainerPort: 32765, Protocol: corev1.ProtocolUDP},
								{Name: "lockd", ContainerPort: 32767, Protocol: corev1.ProtocolTCP},
								{Name: "lockd-udp", ContainerPort: 32767, Protocol: corev1.ProtocolUDP},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{"SYS_ADMIN"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "nfs-data",
									MountPath: cfg.SharePath,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(2049),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       5,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "nfs-data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: cfg.Name + "-data",
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := m.client.AppsV1().Deployments(cfg.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (m *NFSServerManager) createNFSService(ctx context.Context, cfg NFSServerConfig) error {
	// Base ports for all NFS versions
	ports := []corev1.ServicePort{
		{Name: "nfs", Port: 2049, TargetPort: intstr.FromInt(2049), Protocol: corev1.ProtocolTCP},
		{Name: "nfs-udp", Port: 2049, TargetPort: intstr.FromInt(2049), Protocol: corev1.ProtocolUDP},
	}

	// Additional ports for NFSv3 (mountd, rpcbind, statd, lockd)
	if cfg.NFSVersion == "3" {
		ports = append(ports,
			corev1.ServicePort{Name: "mountd", Port: 20048, TargetPort: intstr.FromInt(20048), Protocol: corev1.ProtocolTCP},
			corev1.ServicePort{Name: "mountd-udp", Port: 20048, TargetPort: intstr.FromInt(20048), Protocol: corev1.ProtocolUDP},
			corev1.ServicePort{Name: "rpcbind", Port: 111, TargetPort: intstr.FromInt(111), Protocol: corev1.ProtocolTCP},
			corev1.ServicePort{Name: "rpcbind-udp", Port: 111, TargetPort: intstr.FromInt(111), Protocol: corev1.ProtocolUDP},
			corev1.ServicePort{Name: "statd", Port: 32765, TargetPort: intstr.FromInt(32765), Protocol: corev1.ProtocolTCP},
			corev1.ServicePort{Name: "statd-udp", Port: 32765, TargetPort: intstr.FromInt(32765), Protocol: corev1.ProtocolUDP},
			corev1.ServicePort{Name: "lockd", Port: 32767, TargetPort: intstr.FromInt(32767), Protocol: corev1.ProtocolTCP},
			corev1.ServicePort{Name: "lockd-udp", Port: 32767, TargetPort: intstr.FromInt(32767), Protocol: corev1.ProtocolUDP},
		)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
			Labels: map[string]string{
				"app":         "nfs-server",
				"nfs-version": cfg.NFSVersion,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":         "nfs-server",
				"nfs-version": cfg.NFSVersion,
				"instance":    cfg.Name,
			},
			Ports: ports,
			Type:  corev1.ServiceTypeClusterIP,
		},
	}

	_, err := m.client.CoreV1().Services(cfg.Namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (m *NFSServerManager) waitForDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for deployment %s/%s to become ready", namespace, name)
		}

		deployment, err := m.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if deployment.Status.ReadyReplicas >= 1 && deployment.Status.AvailableReplicas >= 1 {
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}

func (m *NFSServerManager) getServiceClusterIP(ctx context.Context, namespace, name string) (string, error) {
	service, err := m.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return service.Spec.ClusterIP, nil
}

// DeleteNFSServer deletes an NFS server deployment, service, and PVC
func (m *NFSServerManager) DeleteNFSServer(ctx context.Context, namespace, name string) error {
	// Delete deployment
	err := m.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	// Delete service
	err = m.client.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	// Delete PVC
	err = m.client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name+"-data", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete PVC: %w", err)
	}

	return nil
}

// WaitForNFSServerDeletion waits for an NFS server to be completely deleted
func (m *NFSServerManager) WaitForNFSServerDeletion(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for NFS server %s/%s to be deleted", namespace, name)
		}

		// Check if deployment still exists
		_, err := m.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}

// DeployNFSServers deploys NFS servers for all versions (3, 4.1, 4.2) and returns their info
func DeployNFSServers(ctx context.Context, kubeconfig *rest.Config, namespace string) (map[string]*NFSServerInfo, error) {
	manager, err := NewNFSServerManager(kubeconfig)
	if err != nil {
		return nil, err
	}

	servers := make(map[string]*NFSServerInfo)
	versions := []string{"3", "4.1", "4.2"}

	for _, version := range versions {
		serverName := fmt.Sprintf("nfs-server-v%s", sanitizeVersionForName(version))
		cfg := NFSServerConfig{
			Name:       serverName,
			Namespace:  namespace,
			NFSVersion: version,
			SharePath:  "/exports",
		}

		serverInfo, err := manager.DeployNFSServer(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy NFS server v%s: %w", version, err)
		}

		servers[version] = serverInfo
	}

	return servers, nil
}

// DeleteNFSServers deletes all NFS servers in the given namespace
func DeleteNFSServers(ctx context.Context, kubeconfig *rest.Config, namespace string) error {
	manager, err := NewNFSServerManager(kubeconfig)
	if err != nil {
		return err
	}

	versions := []string{"3", "4.1", "4.2"}
	for _, version := range versions {
		serverName := fmt.Sprintf("nfs-server-v%s", sanitizeVersionForName(version))
		if err := manager.DeleteNFSServer(ctx, namespace, serverName); err != nil {
			return fmt.Errorf("failed to delete NFS server v%s: %w", version, err)
		}
	}

	return nil
}

// sanitizeVersionForName converts NFS version to a valid Kubernetes name component
func sanitizeVersionForName(version string) string {
	switch version {
	case "3":
		return "3"
	case "4.1":
		return "4-1"
	case "4.2":
		return "4-2"
	default:
		return version
	}
}
