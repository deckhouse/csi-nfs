/*
Copyright 2024 Flant JSC

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
	"fmt"

	"d8-controller/pkg/controller"
	"d8-controller/pkg/logger"
	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe(controller.NFSStorageClassCtrlName, func() {
	const (
		controllerNamespace = "test-namespace"
		nameForTestResource = "example"
	)
	var (
		ctx = context.Background()
		cl  = NewFakeClient()
		log = logger.Logger{}

		server                     = "192.168.1.100"
		share                      = "/data"
		nfsVer                     = "4.1"
		mountOptForNFSVer          = fmt.Sprintf("nfsvers=%s", nfsVer)
		mountMode                  = "hard"
		mountModeUpdated           = "soft"
		timeout                    = 10
		mountOptForTimeout         = "timeo=10"
		retransmissions            = 3
		mountOptForRetransmissions = "retrans=3"
		readOnlyFalse              = false
		mountOptForReadOnlyFalse   = "rw"
		readOnlyTrue               = true
		mountOptForReadOnlyTrue    = "ro"
		chmodPermissions           = "0777"
	)

	It("Create_nfs_sc_with_all_options", func() {
		nfsSCtemplate := generateNFSStorageClass(NFSStorageClassConfig{
			Name:              nameForTestResource,
			Host:              server,
			Share:             share,
			NFSVersion:        nfsVer,
			MountMode:         mountMode,
			Timeout:           timeout,
			Retransmissions:   retransmissions,
			ReadOnly:          &readOnlyFalse,
			ChmodPermissions:  chmodPermissions,
			ReclaimPolicy:     string(corev1.PersistentVolumeReclaimDelete),
			VolumeBindingMode: string(v1.VolumeBindingWaitForFirstConsumer),
		})

		err := cl.Create(ctx, nfsSCtemplate)
		Expect(err).NotTo(HaveOccurred())

		nsc := &v1alpha1.NFSStorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		Expect(nsc).NotTo(BeNil())
		Expect(nsc.Name).To(Equal(nameForTestResource))
		Expect(nsc.Finalizers).To(HaveLen(0))

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRequeue).To(BeFalse())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSc(sc, server, share, nameForTestResource, controllerNamespace)
		Expect(sc.MountOptions).To(HaveLen(5))
		Expect(sc.MountOptions).To((ContainElements(mountOptForNFSVer, mountMode, mountOptForTimeout, mountOptForRetransmissions, mountOptForReadOnlyFalse)))
		Expect(sc.Parameters).To(HaveLen(5))
		Expect(sc.Parameters).To(HaveKeyWithValue(controller.MountPermissionsParamKey, chmodPermissions))

		secret := &corev1.Secret{}
		err = cl.Get(ctx, client.ObjectKey{Name: controller.SecretForMountOptionsPrefix + nameForTestResource, Namespace: controllerNamespace}, secret)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSecret(secret, nameForTestResource, controllerNamespace)
		Expect(secret.StringData).To(HaveKeyWithValue(controller.MountOptionsSecretKey, fmt.Sprintf("%s,%s,%s,%s,%s", mountOptForNFSVer, mountMode, mountOptForTimeout, mountOptForRetransmissions, mountOptForReadOnlyFalse)))

	})

	It("Annotate_sc_as_default_sc", func() {
		sc := &v1.StorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.Annotations).To(BeNil())

		sc.Annotations = map[string]string{
			controller.StorageClassDefaultAnnotationKey: controller.StorageClassDefaultAnnotationValTrue,
		}

		err = cl.Update(ctx, sc)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.Annotations).To(HaveLen(1))
		Expect(sc.Annotations).To(HaveKeyWithValue(controller.StorageClassDefaultAnnotationKey, controller.StorageClassDefaultAnnotationValTrue))

	})

	It("Update_nfs_sc_1", func() {
		nsc := &v1alpha1.NFSStorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		nsc.Spec.MountOptions.MountMode = mountModeUpdated
		nsc.Spec.MountOptions.ReadOnly = &readOnlyTrue

		err = cl.Update(ctx, nsc)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		Expect(nsc).NotTo(BeNil())
		Expect(nsc.Name).To(Equal(nameForTestResource))
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRequeue).To(BeFalse())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSc(sc, server, share, nameForTestResource, controllerNamespace)
		Expect(sc.MountOptions).To(HaveLen(5))
		Expect(sc.MountOptions).To((ContainElements(mountOptForNFSVer, mountModeUpdated, mountOptForTimeout, mountOptForRetransmissions, mountOptForReadOnlyTrue)))
		Expect(sc.Parameters).To(HaveLen(5))
		Expect(sc.Parameters).To(HaveKeyWithValue(controller.MountPermissionsParamKey, chmodPermissions))

		secret := &corev1.Secret{}
		err = cl.Get(ctx, client.ObjectKey{Name: controller.SecretForMountOptionsPrefix + nameForTestResource, Namespace: controllerNamespace}, secret)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSecret(secret, nameForTestResource, controllerNamespace)
		Expect(secret.StringData).To(HaveKeyWithValue(controller.MountOptionsSecretKey, fmt.Sprintf("%s,%s,%s,%s,%s", mountOptForNFSVer, mountModeUpdated, mountOptForTimeout, mountOptForRetransmissions, mountOptForReadOnlyTrue)))

	})

	It("Check_anotated_sc_after_nsc_update", func() {
		sc := &v1.StorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.Annotations).To(HaveLen(1))
		Expect(sc.Annotations).To(HaveKeyWithValue(controller.StorageClassDefaultAnnotationKey, controller.StorageClassDefaultAnnotationValTrue))

	})

	It("Remove_mount_options_from_nfs_sc", func() {
		nsc := &v1alpha1.NFSStorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		nsc.Spec.MountOptions = nil

		err = cl.Update(ctx, nsc)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		Expect(nsc).NotTo(BeNil())
		Expect(nsc.Name).To(Equal(nameForTestResource))
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRequeue).To(BeFalse())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSc(sc, server, share, nameForTestResource, controllerNamespace)
		Expect(sc.MountOptions).To(HaveLen(1))
		Expect(sc.MountOptions).To((ContainElements(mountOptForNFSVer)))
		Expect(sc.Parameters).To(HaveLen(5))
		Expect(sc.Parameters).To(HaveKeyWithValue(controller.MountPermissionsParamKey, chmodPermissions))

		secret := &corev1.Secret{}
		err = cl.Get(ctx, client.ObjectKey{Name: controller.SecretForMountOptionsPrefix + nameForTestResource, Namespace: controllerNamespace}, secret)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSecret(secret, nameForTestResource, controllerNamespace)
		Expect(secret.StringData).To(HaveKeyWithValue(controller.MountOptionsSecretKey, mountOptForNFSVer))

	})

	It("Add_partial_mount_options_to_nfs_sc", func() {
		nsc := &v1alpha1.NFSStorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		nsc.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{
			MountMode:       mountModeUpdated,
			Retransmissions: retransmissions,
		}

		err = cl.Update(ctx, nsc)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		Expect(nsc).NotTo(BeNil())
		Expect(nsc.Name).To(Equal(nameForTestResource))
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRequeue).To(BeFalse())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSc(sc, server, share, nameForTestResource, controllerNamespace)
		Expect(sc.MountOptions).To(HaveLen(3))
		Expect(sc.MountOptions).To((ContainElements(mountOptForNFSVer, mountModeUpdated, mountOptForRetransmissions)))
		Expect(sc.Parameters).To(HaveLen(5))
		Expect(sc.Parameters).To(HaveKeyWithValue(controller.MountPermissionsParamKey, chmodPermissions))

		secret := &corev1.Secret{}
		err = cl.Get(ctx, client.ObjectKey{Name: controller.SecretForMountOptionsPrefix + nameForTestResource, Namespace: controllerNamespace}, secret)
		Expect(err).NotTo(HaveOccurred())
		performStandartChecksForSecret(secret, nameForTestResource, controllerNamespace)

		Expect(secret.StringData).To(HaveKeyWithValue(controller.MountOptionsSecretKey, fmt.Sprintf("%s,%s,%s", mountOptForNFSVer, mountModeUpdated, mountOptForRetransmissions)))

	})

	It("Remove_nfs_sc", func() {
		nsc := &v1alpha1.NFSStorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Delete(ctx, nsc)
		Expect(err).NotTo(HaveOccurred())

		nsc = &v1alpha1.NFSStorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRequeue).To(BeFalse())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())

		secret := &corev1.Secret{}
		err = cl.Get(ctx, client.ObjectKey{Name: controller.SecretForMountOptionsPrefix + nameForTestResource, Namespace: controllerNamespace}, secret)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())

	})

	It("Create_nfs_sc_when_sc_with_another_provisioner_exists", func() {
		sc := &v1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: nameForTestResource,
			},
			Provisioner: "test-provisioner",
		}

		err := cl.Create(ctx, sc)
		Expect(err).NotTo(HaveOccurred())

		nfsSCtemplate := generateNFSStorageClass(NFSStorageClassConfig{
			Name:              nameForTestResource,
			Host:              server,
			Share:             share,
			NFSVersion:        nfsVer,
			MountMode:         mountMode,
			ReadOnly:          &readOnlyFalse,
			ReclaimPolicy:     string(corev1.PersistentVolumeReclaimDelete),
			VolumeBindingMode: string(v1.VolumeBindingWaitForFirstConsumer),
		})

		err = cl.Create(ctx, nfsSCtemplate)
		Expect(err).NotTo(HaveOccurred())

		nsc := &v1alpha1.NFSStorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).To(HaveOccurred())
		Expect(shouldRequeue).To(BeTrue())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.Provisioner).To(Equal("test-provisioner"))
		Expect(sc.Finalizers).To(HaveLen(0))
		Expect(sc.Labels).To(HaveLen(0))
	})

	It("Remove_nfs_sc_when_sc_with_another_provisioner_exists", func() {
		nsc := &v1alpha1.NFSStorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Delete(ctx, nsc)
		Expect(err).NotTo(HaveOccurred())

		nsc = &v1alpha1.NFSStorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(err).NotTo(HaveOccurred())
		Expect(nsc.Finalizers).To(HaveLen(1))
		Expect(nsc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))
		Expect(nsc.DeletionTimestamp).NotTo(BeNil())

		scList := &v1.StorageClassList{}
		err = cl.List(ctx, scList)
		Expect(err).NotTo(HaveOccurred())

		shouldRequeue, err := controller.RunEventReconcile(ctx, cl, log, scList, nsc, controllerNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRequeue).To(BeFalse())

		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, nsc)
		Expect(k8serrors.IsNotFound(err)).To(BeTrue())

		sc := &v1.StorageClass{}
		err = cl.Get(ctx, client.ObjectKey{Name: nameForTestResource}, sc)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.Provisioner).To(Equal("test-provisioner"))
		Expect(sc.Finalizers).To(HaveLen(0))
		Expect(sc.Labels).To(HaveLen(0))
	})

	// TODO: "Create_nfs_sc_when_sc_with_nfs_provisioner_exists_and_secret_does_not_exists", "Create_nfs_sc_when_sc_does_not_exists_and_secret_exists", "Create_nfs_sc_when_sc_with_nfs_provisioner_exists_and_secret_exists", "Update_nfs_sc_when_sc_with_nfs_provisioner_exists_and_secret_does_not_exists", "Remove_nfs_sc_when_sc_with_nfs_provisioner_exists_and_secret_does_not_exists", "Remove_nfs_sc_when_sc_does_not_exists_and_secret_exists"

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

func performStandartChecksForSc(sc *v1.StorageClass, server, share, nameForTestResource, controllerNamespace string) {
	Expect(sc).NotTo(BeNil())
	Expect(sc.Name).To(Equal(nameForTestResource))
	Expect(sc.Finalizers).To(HaveLen(1))
	Expect(sc.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))
	Expect(sc.Provisioner).To(Equal(controller.NFSStorageClassProvisioner))
	Expect(*sc.ReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimDelete))
	Expect(*sc.VolumeBindingMode).To(Equal(v1.VolumeBindingWaitForFirstConsumer))
	Expect(sc.Parameters).To(HaveKeyWithValue("server", server))
	Expect(sc.Parameters).To(HaveKeyWithValue("share", share))
	Expect(sc.Parameters).To(HaveKeyWithValue(controller.StorageClassSecretNameKey, controller.SecretForMountOptionsPrefix+nameForTestResource))
	Expect(sc.Parameters).To(HaveKeyWithValue(controller.StorageClassSecretNSKey, controllerNamespace))
}

func performStandartChecksForSecret(secret *corev1.Secret, nameForTestResource, controllerNamespace string) {
	Expect(secret).NotTo(BeNil())
	Expect(secret.Name).To(Equal(controller.SecretForMountOptionsPrefix + nameForTestResource))
	Expect(secret.Namespace).To(Equal(controllerNamespace))
	Expect(secret.Finalizers).To(HaveLen(1))
	Expect(secret.Finalizers).To(ContainElement(controller.NFSStorageClassControllerFinalizerName))
}
