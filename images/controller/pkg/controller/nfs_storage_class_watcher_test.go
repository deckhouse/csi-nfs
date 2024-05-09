/*
Copyright 2023 Flant JSC

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

package controller_test

import (
	"context"
	v1alpha1 "d8-controller/api/v1alpha1"
	"d8-controller/pkg/controller"
	"d8-controller/pkg/logger"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe(controller.NFSStorageClassCtrlName, func() {
	const (
		controllerNamespace = "test-namespace"
	)
	var (
		ctx = context.Background()
		cl  = NewFakeClient()
		log = logger.Logger{}

		server              = "192.168.1.100"
		share               = "/data"
		nfsVer              = "4.1"
		mountOptForNFSVer   = fmt.Sprintf("nfsvers=%s", nfsVer)
		mountMode           = "hard"
		readOnly            = false
		mountOptForReadOnly = "rw"
	)

	It("Create_nfs_sc", func() {
		testName := "example"
		//
		nfsSCtemplate := generateNFSStorageClass(NFSStorageClassConfig{
			Name:       testName,
			Host:       server,
			Share:      share,
			NFSVersion: nfsVer,
			MountMode:  mountMode,
			ReadOnly:   &readOnly,
		})

		err := cl.Create(ctx, nfsSCtemplate)
		// if err == nil {
		// 	defer func() {
		// 		if err = cl.Delete(ctx, nfsSCtemplate); err != nil && !errors.IsNotFound(err) {
		// 			fmt.Println(err.Error())
		// 		}
		// 	}()
		// }
		Expect(err).NotTo(HaveOccurred())

		nsc := &v1alpha1.NFSStorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: testName}, nsc)
		Expect(err).NotTo(HaveOccurred())

		if Expect(nsc).NotTo(BeNil()) {
			Expect(nsc.Name).To(Equal(testName))
		}

		scList := &v1.StorageClassList{}
		Expect(err).NotTo(HaveOccurred())
		for _, s := range scList.Items {
			println(fmt.Sprintf("StorageClass: %s", s.Name))
		}

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		println(fmt.Sprintf("RunEventReconcile: shouldRequeue = %t", shouldRequeue))
		Expect(err).NotTo(HaveOccurred())
		// println(err.Error())

		// reconcileTypeForStorageClass, err := controller.IdentifyReconcileFuncForStorageClass(log, scList, nsc, controllerNamespace)
		// println(fmt.Sprintf("reconcileTypeForStorageClass = %s", reconcileTypeForStorageClass))
		// Expect(err).NotTo(HaveOccurred())

		// shouldRequeue, err := controller.ReconcileStorageClassCreateFunc(ctx, cl, log, scList, nsc, controllerNamespace)
		// println(fmt.Sprintf("ReconcileStorageClassCreateFunc: shouldRequeue = %t", shouldRequeue))
		// Expect(err).NotTo(HaveOccurred())

		// secretList := &corev1.SecretList{}
		// err = cl.List(ctx, secretList, client.InNamespace(controllerNamespace))
		// Expect(err).NotTo(HaveOccurred())

		// reconcileTypeForSecret, err := controller.IdentifyReconcileFuncForSecret(log, secretList, nsc, controllerNamespace)
		// println(fmt.Sprintf("reconcileTypeForSecret = %s", reconcileTypeForSecret))
		// Expect(err).NotTo(HaveOccurred())

		// shouldRequeue, err = controller.ReconcileSecretCreateFunc(ctx, cl, log, nsc, controllerNamespace)
		// println(fmt.Sprintf("ReconcileSecretCreateFunc: shouldRequeue = %t", shouldRequeue))
		// Expect(err).NotTo(HaveOccurred())

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: testName}, sc)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(ctx, client.ObjectKey{Name: controller.SecretForMountOptionsPrefix + testName, Namespace: controllerNamespace}, secret)
		Expect(err).NotTo(HaveOccurred())

		Expect(sc).NotTo(BeNil())
		Expect(secret).NotTo(BeNil())
		Expect(sc.Name).To(Equal(testName))
		Expect(secret.Name).To(Equal(controller.SecretForMountOptionsPrefix + testName))
		Expect(sc.Provisioner).To(Equal(controller.NFSStorageClassProvisioner))
		Expect(sc.Parameters).To(HaveKeyWithValue("server", server))
		Expect(sc.Parameters).To(HaveKeyWithValue("share", share))
		Expect(sc.Parameters).To(HaveKeyWithValue(controller.StorageClassSecretNameKey, controller.SecretForMountOptionsPrefix+testName))
		Expect(sc.Parameters).To(HaveKeyWithValue(controller.StorageClassSecretNSKey, controllerNamespace))
		Expect(sc.MountOptions).To((ContainElements(mountMode, mountOptForNFSVer, mountOptForReadOnly)))

		// if Expect(sc).NotTo(BeNil()) {
		// 	Expect(sc.Name).To(Equal(testName))
		// 	Expect(sc.Provisioner).To(Equal(controller.NFSStorageClassProvisioner))
		// }

	})

})

type NFSStorageClassConfig struct {
	Name              string
	Host              string
	Share             string
	NFSVersion        string
	MountMode         string
	Timeout           int
	Retransmissions   int
	ReadOnly          *bool
	ChmodPermissions  string
	ReclaimPolicy     string
	VolumeBindingMode string
}

func generateNFSStorageClass(cfg NFSStorageClassConfig) *v1alpha1.NFSStorageClass {
	return &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: cfg.Name,
		},
		Spec: v1alpha1.NFSStorageClassSpec{
			Connection: &v1alpha1.NFSStorageClassConnection{
				Host:       cfg.Host,
				Share:      cfg.Share,
				NFSVersion: cfg.NFSVersion,
			},
			MountOptions: &v1alpha1.NFSStorageClassMountOptions{
				MountMode:       cfg.MountMode,
				Timeout:         cfg.Timeout,
				Retransmissions: cfg.Retransmissions,
				ReadOnly:        cfg.ReadOnly,
			},
			ChmodPermissions:  cfg.ChmodPermissions,
			ReclaimPolicy:     cfg.ReclaimPolicy,
			VolumeBindingMode: cfg.VolumeBindingMode,
		},
	}
}

func BoolPtr(b bool) *bool {
	return &b
}
