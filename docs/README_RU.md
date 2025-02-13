---
title: "Модуль csi-nfs"
description: "Модуль csi-nfs: общие концепции и положения."
---

Модуль предоставляет CSI для управления NFS-томами и позволяет создавать StorageClass в Kubernetes через [пользовательские ресурсы Kubernetes](./cr.html#nfsstorageclass) `NFSStorageClass`.

{{< alert level="info" >}}
Создание StorageClass для CSI-драйвера `nfs.csi.k8s.io` пользователем запрещено.
{{< /alert >}}

## Системные требования и рекомендации

### Требования

- Используйте стоковые ядра, поставляемые вместе с [поддерживаемыми дистрибутивами](https://deckhouse.ru/documentation/v1/supported_versions.html#linux);
- Убедитесь в наличии развернутого и настроенного NFS-сервера;
- Для поддержки RPC-with-TLS включите в ядре Linux опции `CONFIG_TLS` и `CONFIG_NET_HANDSHAKE`.

### Рекомендации

Чтобы поды модуля перезапускались при изменении параметра `tlsParameters` в настройках модуля, должен быть включен модуль [pod-reloader](https://deckhouse.ru/products/kubernetes-platform/documentation/v1/modules/pod-reloader) (включен по умолчанию).

## Ограничения режима RPC-with-TLS

- Поддерживается только один центр сертификации (CA).
- Для политики безопасности `mtls` поддерживается только один сертификат клиента.
- Один NFS-сервер не может одновременно работать в разных режимах безопасности: `tls`, `mtls` и стандартный режим (без TLS).
- На узлах кластера не должен быть запущен демон `tlshd`, иначе он будет конфликтовать с демоном нашего модуля. Для предотвращения конфликтов при включении TLS на узлах автоматически останавливается сторонний `tlshd` и отключается его автозапуск.

## Быстрый старт

Все команды следует выполнять на машине, имеющей доступ к API Kubernetes с правами администратора.

### Включение модуля

1. Включите модуль `csi-nfs`.  Это приведет к тому, что на всех узлах кластера будет:
   - Зарегистрирован CSI драйвер;
   - Запущены служебные поды компонентов `csi-nfs`.

   ```yaml
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

2. Дождитесь, когда модуль перейдет в состояние `Ready`:

   ```shell
   kubectl get module csi-nfs -w
   ```

### Создание StorageClass

Для создания StorageClass необходимо использовать ресурс [NFSStorageClass](./cr.html#nfsstorageclass). Пример создания ресурса:

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

Для каждого PV будет создаваться каталог `<директория из share>/<имя PV>`.

### Проверка работоспособности модуля

Как проверить работоспособность модуля описано [в FAQ](./faq.html#как-проверить-работоспособность-модуля).
