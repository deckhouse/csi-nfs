---
title: "Модуль csi-nfs: примеры конфигурации"
description: "Использование и примеры настройки csi-nfs"
---

## Быстрый старт

Все команды следует выполнять на машине, имеющей доступ к API Kubernetes с правами администратора.

### Включение модуля

- Включить модуль `csi-nfs`.  Это приведет к тому, что на всех узлах кластера будет:
    - зарегистрирован CSI драйвер;
    - запущены служебные поды компонентов `csi-nfs`.

```shell
kubectl apply -f - <<EOF
apiVersion: deckhouse.io/v1alpha1
kind: ModuleConfig
metadata:
  name: csi-nfs
spec:
  enabled: true
  version: 1
EOF
```

- Дождаться, когда модуль перейдет в состояние `Ready`.

```shell
kubectl get mc csi-nfs -w
```

- Проверить, что в namespace `d8-csi-nfs` все поды в состоянии `Running` или `Completed` и запущены на всех узлах.

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```

### Создание StorageClass

Для создания StorageClass необходимо использовать ресурс [NFSStorageClass](./cr.html#таыstorageclass). Пример команды для создания такого ресурса:

```yaml
kubectl apply -f -<<EOF
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
EOF
```
 
В данном примере не указан `subDir`, поэтому для каждой `PV` будет создаваться каталог по имени `PV` в той директории, которая указана в `share`.

- .  Пример с использованием subDir:

```yaml
kubectl apply -f -<<'EOF'
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.223.187.3
    share: /
    subDir: "${pvc.metadata.namespace}/${pvc.metadata.name}"
    nfsVersion: "4.1"
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
EOF
```

В данном примере на NFS-сервере для каждой PV будет создаваться каталог '<название namespace>/<название PVC>'. Обратите внимание, что название PVC не является уникальным и будет повторяться при пересоздании. В таких случая том для пересоздаваемой PVC будет создаваться в той же директории, и если reclaimPolicy установлен в значение Retain, то данные предыдущих PV будут доступны в новом.
