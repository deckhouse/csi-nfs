## Patches

### Fix go.mod

It fixes https://avd.aquasec.com/nvd/2024/cve-2024-5321/
MUST BE removed after switching to v4.9.0

## How to apply

```bash
export CSI_DRIVER_NFS_VERSION="v4.7.0"
export REPO_PATH=$(pwd)

git clone https://github.com/kubernetes-csi/csi-driver-nfs.git
cd csi-driver-nfs
git checkout ${CSI_DRIVER_NFS_VERSION}
for patchfile in ${REPO_PATH}/images/csi-nfs/patches/*.patch ; do echo "Apply ${patchfile} ... "; git apply ${patchfile}; done

cp -R ${REPO_PATH}/images/csi-nfs/patches/csi-driver-nfs/* ./
```
