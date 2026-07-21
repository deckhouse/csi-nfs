---
title: "Модуль csi-nfs: примеры"
description: Примеры конфигурации модуля csi-nfs.
---

## Быстрый старт

Все команды выполняйте на машине с доступом к API Kubernetes с правами администратора.

### Включение модуля

1. Включите модуль `csi-nfs`. Это приведёт к тому, что на всех узлах кластера будет:
   - зарегистрирован CSI-драйвер;
   - запущены служебные поды компонентов `csi-nfs`.

   ```shell
   d8 k apply -f - <<EOF
   apiVersion: deckhouse.io/v1alpha1
   kind: ModuleConfig
   metadata:
     name: csi-nfs
   spec:
     enabled: true
     version: 1
   EOF
   ```

1. Дождитесь, когда модуль перейдёт в состояние `Ready`:

   ```shell
   d8 k get module csi-nfs -w
   ```

### Создание StorageClass

Для создания StorageClass используйте ресурс [NFSStorageClass](./cr.html#nfsstorageclass). Пример:

```shell
d8 k apply -f - <<EOF
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.223.187.3
    share: /
    nfsVersion: "4.1"
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
  workloadNodes:
    nodeSelector:
      matchLabels:
        storage: "true"
EOF
```

Управляющие поды CSI-драйвера размещаются на узлах кластера согласно суммаризации параметра `workloadNodes` всех NFSStorageClass. При отсутствии параметра `workloadNodes` в NFSStorageClass полезная нагрузка будет размещена на всех узлах.

Для каждого PV будет создаваться каталог `<директория из share>/<имя PV>`.

### Проверка работоспособности модуля

Процесс проверки работоспособности модуля описан в разделе FAQ [Как проверить работоспособность модуля](./faq.html#как-проверить-работоспособность-модуля).

## Конфигурация модуля с поддержкой RPC-with-TLS

```yaml
apiVersion: deckhouse.io/v1alpha1
kind: ModuleConfig
metadata:
  name: csi-nfs
spec:
  enabled: true
  version: 1
  settings:
    tlsParameters:
      ca: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZFVENDQXZtZ...
      mtls:
        clientCert: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1J...
        clientKey: LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCk1JSUpRd0lCQ...
```

## Создание StorageClass с поддержкой RPC-with-TLS

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: nfs-server-name.io
    share: /
    nfsVersion: "4.1"
    tls: true
    mtls: true
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```
