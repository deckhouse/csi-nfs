---
title: "Модуль csi-nfs: FAQ"
description: FAQ по модулю CSI NFS
---

## Как проверить работоспособность модуля?

Для этого необходимо проверить состояние подов в namespace `d8-csi-nfs`. Все поды должны быть в состоянии `Running` или `Completed` и запущены на всех узлах.

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```

## Возможно ли изменение параметров NFS-сервера уже созданных PV?

Нет, данные для подключения к NFS-серверу сохраняются непосредственно в манифесте PV, и не подлежат изменению. Изменение Storage Class также не повлечет изменений настроек подключения в уже существующих PV.

## Как использовать параметр `subDir`?

`subDir` позволяет задавать подпапку для каждого PV. 

### Пример с шаблонами

Можно использовать 3 шаблона:

- `${pvc.metadata.name}`
- `${pvc.metadata.namespace}`
- `${pv.metadata.name}`

```yaml
kubectl apply -f - <<'EOF'
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

В данном примере на NFS-сервере для каждого тома будет создаваться каталог `/<название namespace>/<название PVC>`.

> **Внимание!** Имя PVC задается пользователем. Такие настройки `subDir` могут привести к ситуации, когда имя каталога для вновь создаваемого тома совпадет с именем каталога ранее удаленного тома. Если `reclaimPolicy` установлен в значение `Retain`, то в новом томе будут доступны данные томов, выделенных ранее для PVC с таким же именем.

### Пример без шаблонов

Помимо шаблонов, можно указывать обычную строку - имя подпапки.

```yaml
kubectl apply -f - <<'EOF'
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.223.187.3
    share: /
    subDir: "shared-folder"
    nfsVersion: "4.1"
  reclaimPolicy: Retain
  volumeBindingMode: WaitForFirstConsumer
EOF
```

В данном примере все PV такого StorageClass будут использовать один и тот же каталог на сервере: `/shared-folder`.

> **Внимание!** Если `reclaimPolicy` выставлен в `Delete`, то удаление любой PVC такого StorageClass приведет к удалению всего каталога `/shared-folder`.
