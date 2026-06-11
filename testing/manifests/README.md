# NFS test manifests for csi-nfs

Манифесты для развёртывания тестовых NFS-серверов и `NFSStorageClass` без сборки собственных образов.

## Образы

| Сервер | Образ | Версия NFS |
|--------|-------|------------|
| `nfs-server-v3` | `itsthenetwork/nfs-server-alpine:latest` | v3 |
| `nfs-server-v41` | `ghcr.io/obeone/nfs-server:latest` | v4.1 |
| `nfs-server-v42` | `ghcr.io/obeone/nfs-server:latest` | v4.2 |
| `nfs-server-tls` | `quay.io/rockylinux/rockylinux:9` | v4.2 + RPC-with-TLS |

Для TLS нет готового NFS-образа с `tlshd`: используется публичный Rocky Linux 9, пакеты `nfs-utils` и `ktls-utils` ставятся при старте pod. Pod работает с `hostNetwork: true` — `tlshd` должен видеть netlink handshake ядра.

Перед развёртыванием TLS на узле:

```bash
test -r /proc/net/handshake && echo OK || echo "нет CONFIG_NET_HANDSHAKE"
```

## Требования

- Узлы кластера: Linux, в ядре должны быть собраны модули `nfs` и `nfsd` (`CONFIG_NFS_FS`, `CONFIG_NFSD`). Манифесты поднимают initContainer, который делает `modprobe nfs nfsd` на узле, куда попал pod.
- NFS pod должен попасть на worker-узел (не control-plane), если на master модули NFS отключены.
- Для TLS: ядро с `CONFIG_TLS` и `CONFIG_NET_HANDSHAKE`, на узлах должен работать `tlshd` (модуль `csi-nfs` разворачивает его).
- Для NFS v3: в `ModuleConfig` модуля `csi-nfs` включить `v3support: true` (см. `moduleconfig-v3-example.yaml`). Без этого на worker-узлах не будет `rpcbind`, и монтирование `nfsvers=3` завершится ошибкой.
- NFSv3-сервер публикует порты `2049`, `111`, `32767` (mountd), `32765`/`32766` (statd) — все они должны быть в Service.

## Развёртывание

```bash
kubectl apply -k tests/manifests/
```

Проверка:

```bash
kubectl -n nfs-test get pods,svc
kubectl -n nfs-test wait --for=condition=Ready pod -l app --timeout=300s
kubectl get nfsstorageclass
```

## NFSStorageClass

| Имя | NFS-сервер | Версия | TLS |
|-----|------------|--------|-----|
| `nfs-test-v3` | `nfs-server-v3.nfs-test.svc` | 3 | — |
| `nfs-test-v41` | `nfs-server-v41.nfs-test.svc` | 4.1 | — |
| `nfs-test-v42` | `nfs-server-v42.nfs-test.svc` | 4.2 | — |
| `nfs-test-tls` | `nfs-server-tls.nfs-test.svc` | 4.2 | TLS |
| `nfs-test-tls-mtls` | `nfs-server-tls.nfs-test.svc` | 4.2 | mTLS |

Share в `NFSStorageClass`:
- **v3** — `/exports` (буквальный путь, как в NFSv3)
- **v4.1 / v4.2 / TLS** — `/` (корень NFSv4 pseudo-filesystem; на сервере экспорт `/exports` с `fsid=0`)

## Настройка TLS в csi-nfs

Сертификаты лежат в Secret `nfs-server-tls-certs` (тестовый self-signed CA).

```bash
CA=$(kubectl get secret -n nfs-test nfs-server-tls-certs -o jsonpath='{.data.ca\.crt}')
CLIENT_CERT=$(kubectl get secret -n nfs-test nfs-server-tls-certs -o jsonpath='{.data.client\.crt}')
CLIENT_KEY=$(kubectl get secret -n nfs-test nfs-server-tls-certs -o jsonpath='{.data.client\.key}')
```

Подставьте значения в `moduleconfig-tls-example.yaml` и примените ModuleConfig перед созданием `nfs-test-tls` / `nfs-test-tls-mtls`.

Пересоздать сертификаты:

```bash
./tests/manifests/generate-tls-certs.sh | kubectl apply -f -
```

## Удаление

```bash
kubectl delete -k tests/manifests/
```
