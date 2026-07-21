---
title: "Модуль csi-nfs: FAQ"
description: FAQ по модулю csi-nfs.
---

## Как проверить работоспособность модуля?

Для этого необходимо проверить состояние подов в неймспейсе `d8-csi-nfs`. Все поды должны быть в состоянии `Running` или `Completed` и запущены на всех узлах. Проверьте командой:

```shell
d8 k -n d8-csi-nfs get pod -owide -w
```

## Возможно ли изменение параметров NFS-сервера уже созданных PV?

Нет, данные для подключения к NFS-серверу сохраняются непосредственно в манифесте PV, и не подлежат изменению. Изменение StorageClass также не повлечет изменений настроек подключения в уже существующих PV.

## Как делать снимки томов?

{{< alert level="warning" >}}
**Предостережение об использовании снимков (Volume Snapshots)**

При создании снимков NFS-томов важно понимать схему их создания и связанные ограничения. По возможности избегайте использования снимков в `csi-nfs`:

1. CSI-драйвер создаёт снимок на уровне NFS-сервера.
1. Для этого используется tar, которым упаковывается содержимое тома, со всеми ограничениями, могущими возникнуть из-за этого.
1. **Перед созданием снимка обязательно остановите рабочую нагрузку** (поды), использующую NFS-том.
1. NFS не обеспечивает атомарность операций на уровне файловой системы при создании снимка.

{{< /alert >}}

В `csi-nfs` снимки создаются путём архивирования директории тома. Архив сохраняется в корне директории NFS-сервера, указанной в параметре `spec.connection.share`.

1. Включите `snapshot-controller`:

   ```shell
   d8 k apply -f - <<EOF
   apiVersion: deckhouse.io/v1alpha1
   kind: ModuleConfig
   metadata:
     name: snapshot-controller
   spec:
     enabled: true
     version: 1
   EOF
   ```

1. Создайте снимки томов. Для этого выполните следующую команду, указав нужные параметры:

   ```shell
   d8 k apply -f - <<EOF
   apiVersion: snapshot.storage.k8s.io/v1
   kind: VolumeSnapshot
   metadata:
     name: my-snapshot
     namespace: <имя неймспейса, в котором находится PVC>
   spec:
     volumeSnapshotClassName: csi-nfs-snapshot-class
     source:
       persistentVolumeClaimName: <имя PVC, для которого необходимо создать снимок>
   EOF
   ```

1. Проверьте состояние созданного снимка командой:

   ```shell
   d8 k get volumesnapshot
   ```

Эта команда покажет список всех снимков и их текущее состояние.


## Как выбрать метод очистки тома перед удалением PV

{{< alert level="warning" >}}
**Функционал очистки тома доступен только в коммерческих редакциях Deckhouse Kubernetes Platform.**
{{< /alert >}}

На удаляемом томе могут остаться файлы с пользовательскими данными. Эти файлы будут удалены и не будут доступны другим пользователям через NFS.

Однако данные удалённых файлов могут оказаться доступными другим клиентам, если сервер предоставит доступ к своему хранилищу на уровне блочных устройств.

Выбрать метод очистки тома перед удалением поможет параметр `volumeCleanup`.

{{< alert level="warning" >}}
Эта опция не влияет на файлы, уже удалённые клиентским приложением.

Эта опция влияет только на команды, отправляемые по протоколу NFS. Выполнение этих команд на стороне сервера определено:

- сервисом NFS-сервера;
- файловой системой;
- уровнем блочных устройств и их виртуализации (например, LVM);
- самими физическими устройствами.

Убедитесь в доверенности сервера. Не отправляйте деликатные данные на серверы, в которых нет уверенности.
{{< /alert >}}

### Метод SinglePass

Используется, если для параметра `volumeCleanup` задано значение `RandomFillSinglePass`.

Содержимое файлов переписывается случайной последовательностью перед удалением. Случайная последовательность передаётся по сети.

### Метод ThreePass

Используется, если для параметра `volumeCleanup` задано значение `RandomFillThreePass`.

Содержимое файлов трижды переписывается случайной последовательностью перед удалением. Три случайных последовательности передаются по сети.

### Метод Discard

Используется, если для параметра `volumeCleanup` задано значение `Discard`.

Многие файловые системы реализуют поддержку твердотельных накопителей, позволяя освободить место, занятое файлом, на блочном уровне без записи новых данных для увеличения срока службы твердотельного накопителя. Однако не все накопители гарантируют недоступность данных освобождённых блоков.

Если для `volumeCleanup` установлено значение `Discard`, содержимое файлов помечается как свободное через системный вызов `falloc` с флагом `FALLOC_FL_PUNCH_HOLE`. Файловая система освободит полностью используемые файлом блоки через вызов `blkdiscard`, а остальное место будет перезаписано нулями.

Преимущества этого метода:

- объём трафика не зависит от размера файлов, а только от их количества;
- метод может обеспечить недоступность старых данных при некоторых конфигурациях сервера;
- работает как для жёстких дисков, так и для твердотельных накопителей;
- позволяет увеличить время жизни твердотельного накопителя.

## Почему не удаляются PV, созданные в StorageClass с поддержкой RPC-with-TLS, а вместе с ними и каталоги <имя PV> на NFS-сервере?

Если ресурс [NFSStorageClass](./cr.html#nfsstorageclass) был настроен с поддержкой RPC-with-TLS, может возникнуть ситуация, когда PV не удастся удалить.
Это происходит из-за удаления секрета (например, после удаления `NFSStorageClass`), который хранит параметры монтирования. В результате контроллер не может смонтировать NFS-директорию для удаления директории `<имя PV>`.

## Как в настройках ModuleConfig в параметре tlsParameters.ca разместить несколько CA?

- для двух CA
```shell
cat CA1.crt CA2.crt | base64 -w0
```

- для трех CA
```shell
cat CA1.crt CA2.crt CA3.crt | base64 -w0
```

- и т.д.

## Какие требования к дистрибутиву Linux для развёртывания NFS-сервера с поддержкой RPC-with-TLS?

- Ядро должно быть собрано с включёнными параметрами `CONFIG_TLS` и `CONFIG_NET_HANDSHAKE`;
- Пакет nfs-utils (в дистрибутивах основанных на Debian - nfs-common) должен быть >= 2.6.3.



## Установка NFS-сервера с поддержкой RPC-with-TLS 2.7.1 на РЕД ОС 8


### Генерация TLS-сертификатов (опционально)
#### Генерация корневого сертификата CA

```shell
# mkdir /etc/ssl/tlshd
# cd /etc/ssl/tlshd
# openssl genrsa -out ca.key 4096
# openssl req -x509 -new -nodes -key ca.key -sha256 -days 1826 -out ca.crt -subj "/CN=sidorov/O=Flant"
```

#### Генерация серверного сертификата, где **10.0.5.111** — IP-адрес NFS-сервера

```shell
# openssl req -new -nodes -out nfs_tlshd.csr -newkey rsa:4096 -keyout nfs_tlshd.key -subj "/CN=nfs/O=Flant"

# cat > "nfs.v3.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names
[alt_names]
IP.0 = 10.0.5.111
EOF

# openssl x509 -req -in nfs_tlshd.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out nfs.crt -days 730 -sha256 -extfile nfs.v3.ext
```

#### Генерация клиентского сертификата

```shell
# openssl req -new -nodes -out nfs_client.csr -newkey rsa:4096 -keyout nfs_client.key -subj "/CN=nfs-client/O=Flant"

# cat > "nfs_client.v3.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names
[alt_names]
IP.0 = 10.0.5.117
EOF

# openssl x509 -req -in nfs_client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out nfs_client.crt -days 730 -sha256 -extfile nfs_client.v3.ext
```

### Установка tlshd

#### Проверка ядра

Для работы RPC-with-TLS необходимо Linux ядро старше 6.4:

```shell
# uname -r
6.6.76-1.red80.x86_64
```

Ядро скомпилировано с необходимыми параметрами:

```shell
# grep -P 'CONFIG_TLS|CONFIG_NET_HANDSHAK' /boot/config-$(uname -r)
CONFIG_TLS=m
CONFIG_TLS_DEVICE=y
# CONFIG_TLS_TOE is not set
CONFIG_NET_HANDSHAKE=y
```

 

#### Сборка tlshd из исходных кодов

Установка зависимостей: 

```shell
# dnf install automake gcc gnutls-devel keyutils-libs-devel libnl3-devel glib2-devel wget
```

Скачайте исходный код и скомпилируйте:

```shell
# wget https://github.com/oracle/ktls-utils/releases/download/ktls-utils-0.11/ktls-utils-0.11.tar.gz
# tar xf ktls-utils-0.11.tar.gz 
# cd ktls-utils-0.11/ 
# ./autogen.sh 
# ./configure —with-systemd 
# make 
# make install 
# systemctl daemon-reload 
# systemctl enable —now tlshd
```

### Конфигурация tlshd на использование TLS

Отредактируйте конфигурацию tlshd:

```shell
# cat /etc/tlshd.conf
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
```

Перезапустите сервис tlshd:

```shell
# systemctl restart tlshd
```

### Установка nfs-utils версии 2.7.1

Проверьте, что NFS включён в ядре:

```shell
# grep -P 'CONFIG_NFSD_V4|CONFIG_NETWORK_FILESYSTEMS|CONFIG_NFS_FS|CONFIG_NFSD' /boot/config-$(uname -r)
CONFIG_NETWORK_FILESYSTEMS=y
CONFIG_NFS_FS=m
CONFIG_NFS_FSCACHE=y
CONFIG_NFSD=m
# CONFIG_NFSD_V2 is not set
CONFIG_NFSD_V3_ACL=y
CONFIG_NFSD_V4=y
CONFIG_NFSD_PNFS=y
CONFIG_NFSD_BLOCKLAYOUT=y
CONFIG_NFSD_SCSILAYOUT=y
CONFIG_NFSD_FLEXFILELAYOUT=y
CONFIG_NFSD_V4_2_INTER_SSC=y
CONFIG_NFSD_V4_SECURITY_LABEL=y
```

Сборка nfs-utils из исходных кодов:

Установите зависимости:

```shell
# dnf install libxml2-devel rpcgen libtirpc-devel libuuid-devel libevent-devel sqlite-devel device-mapper-devel sssd-krb5-common krb5-devel gssproxy libev libnfsidmap libverto-libev quota quota-nls rpcbind sssd-nfs-idmap
```

 

Скачайте исходный код и скомпилируйте:

```shell
# wget https://www.kernel.org/pub/linux/utils/nfs-utils/2.7.1/nfs-utils-2.7.1.tar.gz
# tar xf nfs-utils-2.7.1.tar.gz
# cd nfs-utils-2.7.1
# ./configure --prefix=/usr --sysconfdir=/etc/ --sbindir=/usr/sbin --enable-nfsv4server —with-systemd
# make
# make install
# chmod u+w,go+r /usr/sbin/mount.nfs
```

Создайте NFS-шару и сконфигурируйте её:

```shell
# echo '/mnt/shared 10.0.5.0/24(rw,sync,no_subtree_check,no_root_squash,xprtsec=mtls)' >> /etc/exports
# exportfs -a
# exportfs -v
/mnt/shared     10.0.5.0/24(sync,wdelay,hide,no_subtree_check,sec=sys,rw,secure,no_root_squash,no_all_squash,xprtsec=mtls)
```

### Настройка csi-nfs

Включите модуль с настроенным TLS:

```yaml
apiVersion: deckhouse.io/v1alpha1
kind: ModuleConfig
metadata:
  name: csi-nfs
spec:
  enabled: true
  settings:
    tlsParameters:
      ca: <cat ca.crt | base64 -w0>
      mtls:
        clientCert: <cat nfs_client.crt | base64 -w0>
        clientKey: <cat nfs_client.key | base64 -w0>
  version: 1
```

Дождитесь, когда модуль перейдёт в состояние `Ready`:

```shell
# d8 k get module csi-nfs -w
```

Создайте NFSStorageClass:

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.0.5.111
    share: /mnt/shared
    nfsVersion: "4.2"
    mtls: true
    tls: true
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```

### Тестирование работы

Создайте Deployment с заказом диска в созданном NFS:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: nfs-one
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  namespace: nfs-one
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        volumeMounts:
        - name: nfs-volume
          mountPath: /mnt/share
      volumes:
      - name: nfs-volume
        persistentVolumeClaim:
          claimName: nfs-pvc
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nfs-pvc
  namespace: nfs-one
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 1Gi
  storageClassName: nfs-storage-class
```

В логах tlshd будет информация об успешном подключении к NFS-серверу

```shell
# journalctl -fu tlshd
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: The certificate is trusted.
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: The peer offered 1 certificate(s).
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: Session description: (TLS1.3)-(ECDHE-SECP384R1)-(RSA-PSS-RSAE-SHA384)-(AES-256-GCM)
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: Handshake with unknown (10.0.5.110) was successful
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: Querying the handshake service
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: Parsing a valid netlink message
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: No peer identities found
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: No certificates found
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Name or service not known
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: System config file: /etc/crypto-policies/back-ends/gnutls.config
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Server x.509 truststore is /etc/ssl/tlshd/ca.crt
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: System trust: Loaded 1 certificate(s).
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Retrieved 1 x.509 server certificate(s) from /etc/ssl/tlshd/nfs.crt
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Retrieved private key from /etc/ssl/tlshd/nfs_tlshd.key
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: gnutls(2): checking 13.02 (GNUTLS_AES_256_GCM_SHA384) for compatibility
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: gnutls(2): Selected (RSA) cert
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: gnutls(2): EXT[0x384dc0d0]: server generated SECP384R1 shared key
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: The certificate is trusted.
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: The peer offered 1 certificate(s).
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Session description: (TLS1.3)-(ECDHE-SECP384R1)-(RSA-PSS-RSAE-SHA384)-(AES-256-GCM)
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Handshake with unknown (10.0.5.117) was successful
```
