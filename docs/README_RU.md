---
title: "Модуль csi-nfs"
description: "Модуль csi-nfs: общие концепции и положения."
moduleStatus: experimental
---

Модуль предоставляет CSI для управления томамим на основе `NFS`. Модуль позволяет создавать `StorageClass` в `Kubernetes` через создание [пользовательских ресурсов Kubernetes](./cr.html) `NFSStorageClass`.

> **Внимание!** Создание `StorageClass` для CSI-драйвера `nfs.csi.k8s.io` пользователем запрещено.

Инструкции по использованию можно найти [здесь]((./usage.html))

## Системные требования и рекомендации

### Требования

- Использование стоковых ядер, поставляемых вместе с [поддерживаемыми дистрибутивами](https://deckhouse.ru/documentation/v1/supported_versions.html#linux);
- Наличие развернутого и настроенного `NFS` сервера.
