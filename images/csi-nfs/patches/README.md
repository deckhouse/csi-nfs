# Patches

## 001-add-volume-cleanup-and-feature-pkg.patch

Add volume cleanup feature

## 002-add-mountPermissions-to-snapshot.patch

Add mountPermissions to snapshot. Fix mountPermissions.

## 003-Updated-golang-version.patch

Update golang version

## 004-add-socket-permissions.patch

Add sockerPermissions to csi-controller

## How to apply

```bash
export CSI_DRIVER_NFS_VERSION="v4.11.0"
export REPO_PATH=$(pwd)

git clone https://github.com/kubernetes-csi/csi-driver-nfs.git
cd csi-driver-nfs
git checkout ${CSI_DRIVER_NFS_VERSION}
for patchfile in ${REPO_PATH}/images/csi-nfs/patches/*.patch ; do echo "Apply ${patchfile} ... "; git apply ${patchfile}; done

cp -R ${REPO_PATH}/images/csi-nfs/patches/csi-driver-nfs/* ./
```
