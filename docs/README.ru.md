---
title: "Модуль csi-nfs"
description: "Модуль csi-nfs: общие концепции и положения."
---

Модуль `csi-nfs` предоставляет CSI-драйвер для управления NFS-томами в Kubernetes.
С его помощью можно создавать PersistentVolume на NFS-сервере через [кастомные ресурсы NFSStorageClass](./cr.html#nfsstorageclass).

## Основные возможности

Модуль `csi-nfs` предоставляет следующие возможности:

- Создание PersistentVolume на базе NFS через кастомный ресурс NFSStorageClass.
- Поддержка режимов доступа RWO и RWX, включая RWX в Deckhouse Virtualization Platform.
- Ограничение монтирования томов выбранными узлами кластера через [параметр `workloadNodes`](cr.html#nfsstorageclass-v1alpha1-spec-workloadnodes).
- Поддержка режима RPC-with-TLS (`tls` / `mtls`) для подключений к NFS-серверу (в коммерческих редакциях DKP).
- Очистка данных тома перед удалением PV с помощью [параметра `volumeCleanup`](cr.html#nfsstorageclass-v1alpha1-spec-volumecleanup) (в коммерческих редакциях DKP).

{{< alert level="info" >}}
StorageClass для CSI-драйвера `nfs.csi.k8s.io` создаются только через ресурс [NFSStorageClass](./cr.html#nfsstorageclass). Создание обычных ресурсов StorageClass для этого CSI-драйвера запрещено.
{{< /alert >}}

## Системные требования и рекомендации

### Требования

Перед использованием модуля убедитесь в следующем:

- Используйте стоковые ядра, поставляемые вместе с [поддерживаемыми дистрибутивами](/products/kubernetes-platform/documentation/v1/supported_versions.html#linux);
- Убедитесь, что NFS-сервер корректно настроен и запущен:
  - Для модулей DKP, в настройках которых используется StorageClass, может потребоваться разрешить доступ клиентам с root-правами. В Linux это реализуется через опцию `no_root_squash`. В других операционных системах и СХД аналогичная настройка может иметь иное название;
  - Для хранилища виртуальных дисков в [Deckhouse Virtualization Platform](/products/virtualization-platform/documentation/) опция `no_root_squash` обязательна.
- Для поддержки RPC-with-TLS включите в ядре Linux опции `CONFIG_TLS` и `CONFIG_NET_HANDSHAKE`.
- Для работы модуля должен быть включён модуль [`snapshot-controller`](/modules/snapshot-controller/).

### Рекомендации

Чтобы поды модуля перезапускались при изменении [параметра `tlsParameters`](configuration.html#parameters-tlsparameters), убедитесь, что включён модуль [`pod-reloader`](/modules/pod-reloader/) (включён по умолчанию).

## Ограничения

### Создание снимков томов

При создании снимков NFS-томов важно понимать схему их создания и связанные ограничения. По возможности избегайте использования снимков в `csi-nfs`:

1. CSI-драйвер создаёт снимок на уровне NFS-сервера.
1. Для этого используется tar, которым упаковывается содержимое тома, со всеми ограничениями, могущими возникнуть из-за этого.
1. **Перед созданием снимка обязательно остановите рабочую нагрузку** (поды), использующую NFS-том.
1. NFS не обеспечивает атомарность операций на уровне файловой системы при создании снимка.

### Ограничения режима RPC-with-TLS

Для режима RPC-with-TLS действуют следующие ограничения:

- Для политики безопасности `mtls` поддерживается только один сертификат клиента.
- Один NFS-сервер не может одновременно работать в разных режимах безопасности: `tls`, `mtls` и стандартный режим (без TLS).
- На узлах кластера не должен быть запущен демон `tlshd`, иначе он будет конфликтовать с демоном модуля. Для предотвращения конфликтов при включении TLS на узлах автоматически останавливается сторонний `tlshd` и отключается его автозапуск.

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

Управляющие поды CSI-драйвера размещаются на узлах кластера согласно суммаризации [параметра `workloadNodes`](cr.html#nfsstorageclass-v1alpha1-spec-workloadnodes) всех NFSStorageClass. При отсутствии параметра `workloadNodes` в NFSStorageClass полезная нагрузка будет размещена на всех узлах.

Для каждого PV будет создаваться каталог `<директория из share>/<имя PV>`.
