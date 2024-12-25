---
title: "Модуль csi-nfs: FAQ"
description: FAQ по модулю CSI NFS
---

## Как проверить работоспособность модуля?

Для этого необходимо проверить состояние подов в пространстве имён `d8-csi-nfs`. Все поды должны быть в состоянии `Running` или `Completed`, и запущены на всех узлах. Проверить можно командой:

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```

## Возможно ли изменение параметров NFS-сервера уже созданных PV?

Нет, данные для подключения к NFS-серверу сохраняются непосредственно в манифесте PV, и не подлежат изменению. Изменение StorageClass также не повлечет изменений настроек подключения в уже существующих PV.

## Как делать снимки томов (снапшоты)?

В `csi-nfs` снимки создаются путем архивирования папки тома. Архив сохраняется в корне папки NFS-сервера, указанной в параметре `spec.connection.share`.

1. Включите `snapshot-controller`:

   ```yaml
   kubectl apply -f -<<EOF
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

   ```yaml
   kubectl apply -f -<<EOF
   apiVersion: snapshot.storage.k8s.io/v1
   kind: VolumeSnapshot
   metadata:
     name: my-snapshot
     namespace: <имя namespace, в котором находится PVC>
   spec:
     volumeSnapshotClassName: csi-nfs-snapshot-class
     source:
       persistentVolumeClaimName: <имя PVC, для которого необходимо создать снимок>
   EOF
   ```

1. Проверьте состояние созданного снимка командой:

   ```shell
   kubectl get volumesnapshot
   ```

Эта команда покажет список всех снимков и их текущее состояние.

## Почему не удаляются PV созданные в StorageClass с поддержкой RPC-with-TLS, а вместе с ними и каталоги `<имя PV>` на NFS сервере?

Если ресурс [NFSStorageClass](./cr.html#nfsstorageclass) был настроен с поддержкой RPC-with-TLS, может возникнуть ситуация, когда PV не удастся удалить.

Это происходит из-за удаления секрета (например, после удаления `NFSStorageClass`), который хранит параметры монтирования. В результате контроллер не может смонтировать NFS-папку для удаления папки `<имя PV>`.
