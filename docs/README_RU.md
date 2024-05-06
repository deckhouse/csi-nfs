---
title: "Модуль csi-nfs"
description: "Модуль csi-nfs: общие концепции и положения."
moduleStatus: experimental
---

Модуль предоставляет CSI для управления томамим на основе `NFS`.

> **Внимание!** Создание `StorageClass` для CSI-драйвера nfs.csi.storage.deckhouse.io пользователем запрещено.

## Быстрый старт

Все команды следует выполнять на машине, имеющей доступ к API Kubernetes с правами администратора.

### Включение модулей

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