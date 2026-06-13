# NFS test manifests for csi-nfs

Манифесты для развёртывания тестовых NFS-серверов (v3, v4.1, v4.2) в кластере и `NFSStorageClass` без сборки собственных образов.

TLS-сервер в кластер **не** разворачивается — его поднимают на отдельной машине (см. раздел ниже).

## Образы

| Сервер | Образ | Версия NFS |
|--------|-------|------------|
| `nfs-server-v3` | `ghcr.io/obeone/nfs-server:latest` | v3 |
| `nfs-server-v41` | `ghcr.io/obeone/nfs-server:latest` | v4.1 |
| `nfs-server-v42` | `ghcr.io/obeone/nfs-server:latest` | v4.2 |

## Требования

- Узлы кластера: Linux, в ядре должны быть собраны модули `nfs` и `nfsd` (`CONFIG_NFS_FS`, `CONFIG_NFSD`). Манифесты поднимают initContainer, который делает `modprobe nfs nfsd` на узле, куда попал pod.
- NFS pod должен попасть на worker-узел (не control-plane), если на master модули NFS отключены.
- Для NFS v3: в `ModuleConfig` модуля `csi-nfs` включить `v3support: true` (см. `moduleconfig-v3-example.yaml`). Без этого на CSI-узлах не будет `rpcbind`, и монтирование `nfsvers=3` завершится ошибкой.
- NFSv3-сервер работает с `hostNetwork: true` — mountd/statd регистрируются на IP узла. `NFSStorageClass` для v3 **нельзя** указывать Service DNS (`nfs-server-v3.nfs-test.svc`): mount падает с `Protocol not supported`. Host должен быть InternalIP узла, куда попал pod (скрипт `apply-nfs-test-v3.sh`).
- На single-node кластере (только master) CSI монтирует с control-plane — там же нужны `rpcbind`, `nfs-common` и метка `storage.deckhouse.io/csi-nfs-node`.
- NFSv3-сервер публикует порты `2049`, `111`, `20048` (mountd), `32765`/`32766` (statd).

## Развёртывание в кластере

```bash
kubectl apply -k testing/manifests/
./testing/manifests/apply-nfs-test-v3.sh
```

`apply-nfs-test-v3.sh` создаёт `NFSStorageClass nfs-test-v3` с `host` = InternalIP узла, где запущен `nfs-server-v3`. Если CR уже есть с другим host (immutable), скрипт удалит и пересоздаст его.

Проверка:

```bash
kubectl -n nfs-test get pods,svc
kubectl -n nfs-test wait --for=condition=Ready pod -l app --timeout=300s
kubectl get nfsstorageclass
```

## NFSStorageClass (in-cluster)

| Имя | NFS-сервер | Версия | share |
|-----|------------|--------|-------|
| `nfs-test-v3` | InternalIP узла с `nfs-server-v3` (см. `apply-nfs-test-v3.sh`) | 3 | `/exports` |
| `nfs-test-v41` | `nfs-server-v41.nfs-test.svc` | 4.1 | `/` |
| `nfs-test-v42` | `nfs-server-v42.nfs-test.svc` | 4.2 | `/` |

Share в `NFSStorageClass`:
- **v3** — `/exports` (буквальный путь, как в NFSv3)
- **v4.1 / v4.2** — `/` (корень NFSv4 pseudo-filesystem; на сервере экспорт `/exports` с `fsid=0`)

## NFSv3: диагностика `Protocol not supported`

1. `v3support: true` в ModuleConfig:

```bash
kubectl get moduleconfig csi-nfs -o jsonpath='{.spec.settings.v3support}{"\n"}'
```

2. На узле, где крутится CSI NFS (в логе provisioner видно `csi-nfs-test-master-0` — это master), должны быть `rpcbind` и метка `storage.deckhouse.io/csi-nfs-node`:

```bash
NODE=csi-nfs-test-master-0   # подставьте свой узел
kubectl get node "$NODE" --show-labels | grep csi-nfs-node
ssh "$NODE" 'systemctl is-active rpcbind; ls -la /run/rpcbind.sock'
```

Если `rpcbind` не active — `sudo systemctl enable --now rpcbind rpc-statd` (NGC модуля ставит пакеты, но не всегда поднимает `rpcbind`).

3. `NFSStorageClass` с Service DNS вместо IP узла — типичная причина ошибки. Пересоздайте через скрипт:

```bash
kubectl delete nfsstorageclass nfs-test-v3 --ignore-not-found
./testing/manifests/apply-nfs-test-v3.sh
```

4. Сервер отдаёт v3:

```bash
kubectl -n nfs-test exec deploy/nfs-server-v3 -- cat /proc/fs/nfsd/versions
# ожидается +3
```

5. Ручной mount **с того же узла, где CSI** (InternalIP, не Service DNS):

```bash
NODE_IP=$(kubectl -n nfs-test get pod -l app=nfs-server-v3 -o jsonpath='{.items[0].status.hostIP}')
sudo mkdir -p /mnt/nfsv3-test
sudo mount -t nfs -o nfsvers=3,proto=tcp,nolock "${NODE_IP}:/exports" /mnt/nfsv3-test
sudo umount /mnt/nfsv3-test
```

6. После исправления host пересоздайте PVC (старый claim мог остаться в `ProvisioningFailed`).

## NFS-сервер с TLS на отдельной машине

В pod'е `tlshd` не работает стабильно (`Kernel handshake service is not available`). Для тестов TLS поднимайте NFS-сервер **на отдельной VM**, не в Kubernetes.

Подходит Ubuntu 24.04 со стандартным ядром 6.8.0 — отдельно включать TLS в ядре не нужно.

### 1. Проверка ядра

```bash
uname -r
grep -E 'CONFIG_TLS|CONFIG_NET_HANDSHAKE' /boot/config-$(uname -r)
test -r /proc/net/handshake && echo OK
```

### 2. Пакеты (Ubuntu 24.04)

```bash
sudo apt update
sudo apt install -y nfs-kernel-server ktls-utils nfs-common rpcbind

dpkg -l nfs-common ktls-utils | awk '/^ii/'
# nfs-common >= 2.6.3, ktls-utils >= 0.11
```

Если `ktls-utils` в репозитории старее 0.11:

```bash
wget https://archive.ubuntu.com/ubuntu/pool/universe/k/ktls-utils/ktls-utils_0.11-1_amd64.deb
sudo dpkg -i ktls-utils_0.11-1_amd64.deb
```

### 3. Сертификаты

```bash
sudo mkdir -p /etc/ssl/tlshd
cd /etc/ssl/tlshd

sudo openssl genrsa -out ca.key 4096
sudo openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 \
  -out ca.crt -subj "/CN=nfs-test-ca/O=Test"

sudo openssl genrsa -out nfs_tlshd.key 4096
sudo openssl req -new -nodes -key nfs_tlshd.key -out nfs.csr \
  -subj "/CN=nfs-server/O=Test"

cat > nfs.ext <<'EOF'
subjectAltName=DNS:nfs-tls.example,DNS:nfs-tls,IP:10.0.0.10
EOF
# подставьте IP/DNS вашей VM

sudo openssl x509 -req -in nfs.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out nfs.crt -days 3650 -sha256 -extfile nfs.ext
```

Для mTLS — дополнительно client-сертификат, подписанный тем же CA (см. [FAQ модуля](../../docs/FAQ.md)).

### 4. tlshd

```bash
sudo tee /etc/tlshd.conf >/dev/null <<'EOF'
[debug]
loglevel=2
tls=2
nl=0

[authenticate]

[authenticate.client]

[authenticate.server]
x509.truststore= /etc/ssl/tlshd/ca.crt
x509.certificate= /etc/ssl/tlshd/nfs.crt
x509.private_key= /etc/ssl/tlshd/nfs_tlshd.key
EOF

sudo systemctl enable --now tlshd
sudo systemctl status tlshd
```

### 5. Экспорт NFS

```bash
sudo mkdir -p /exports
sudo chmod 1777 /exports

grep -q '^/exports ' /etc/exports || \
  echo '/exports *(rw,sync,no_subtree_check,no_root_squash,insecure,xprtsec=tls)' | sudo tee -a /etc/exports

sudo exportfs -arv
sudo systemctl enable --now rpcbind nfs-kernel-server
```

На Ubuntu сервис NFS — `nfs-kernel-server`, не `nfs-server`.

Проверка:

```bash
sudo exportfs -v
ss -tlnp | grep -E '2049|111'
sudo journalctl -u tlshd -n 20
```

### 6. csi-nfs в кластере

На worker-узлах **не** включайте системный `tlshd` — модуль `csi-nfs` поднимает свой. Конфликт с системным `tlshd` на узлах кластера возможен, если он запущен вручную.

В `ModuleConfig` передайте CA (и client cert/key для mTLS):

```bash
cat /etc/ssl/tlshd/ca.crt | base64 -w0      # → tlsParameters.ca
cat client.crt | base64 -w0                 # → tlsParameters.mtls.clientCert
cat client.key | base64 -w0                 # → tlsParameters.mtls.clientKey
```

Шаблон: `moduleconfig-tls-example.yaml`.

### 7. NFSStorageClass для внешнего TLS-сервера

Поля `host` и `share` immutable — создайте CR с IP/DNS внешней VM:

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-test-tls
spec:
  connection:
    host: 10.0.0.10          # IP Ubuntu VM с TLS-сервером
    share: /
    nfsVersion: "4.2"
    tls: true
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```

Для mTLS добавьте `mtls: true` и client-сертификаты в `ModuleConfig`.

Share `/` — экспорт `/exports` с `fsid=0` на сервере (как для v4.2 in-cluster).

### 8. Проверка mount с клиента

```bash
sudo cp /etc/ssl/tlshd/ca.crt /usr/local/share/ca-certificates/nfs-test-ca.crt
sudo update-ca-certificates

sudo mkdir -p /mnt/nfs-tls
sudo mount -t nfs4 -o xprtsec=tls 10.0.0.10:/ /mnt/nfs-tls
sudo journalctl -u tlshd -n 20
```

## Удаление in-cluster серверов

```bash
kubectl delete -k testing/manifests/
```
