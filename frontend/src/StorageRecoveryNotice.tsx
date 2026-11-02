import { useEffect, useState } from "react";
import type { DesktopBridge, StorageRecoveryStatus } from "./contracts";
import { startDiagnosticPolling } from "./diagnosticPolling";

export function recoverySignature(status: StorageRecoveryStatus | null) {
  if (!status || (!status.quarantinedFavorites && !status.quarantinedHistory && !status.settingsRecovered)) return "";
  return `${status.quarantinedFavorites}:${status.quarantinedHistory}:${status.settingsRecovered}`;
}

export function StorageRecoveryMessage({
  status,
  dismissed = new Set<string>(),
  onDismiss,
}: {
  status: StorageRecoveryStatus | null;
  dismissed?: ReadonlySet<string>;
  onDismiss?: (signature: string) => void;
}) {
  const signature = recoverySignature(status);
  if (!status || !signature || dismissed.has(signature)) return null;
  const count = status.quarantinedFavorites + status.quarantinedHistory;
  return (
    <div className="storage-recovery-notice">
      <p role="status">
        {status.settingsRecovered && "本地设置曾发现异常，已恢复默认设置并保留原始记录。"}
        {count > 0 && `有 ${count} 条本地收藏或历史记录异常，已隔离保留。`}
        其他本地资料可以继续使用。
      </p>
      {onDismiss && (
        <button type="button" aria-label="知道了，本次会话不再提示这次恢复" onClick={() => onDismiss(signature)}>
          知道了
        </button>
      )}
    </div>
  );
}

export function StorageRecoveryNotice({ bridge }: { bridge: DesktopBridge }) {
  const [status, setStatus] = useState<StorageRecoveryStatus | null>(null);
  const [dismissed, setDismissed] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => {
    if (!bridge.GetStorageRecoveryStatus) return;
    return startDiagnosticPolling(() => bridge.GetStorageRecoveryStatus!(), setStatus);
  }, [bridge]);
  return (
    <StorageRecoveryMessage
      status={status}
      dismissed={dismissed}
      onDismiss={(signature) => {
        setDismissed((previous) => new Set(previous).add(signature));
      }}
    />
  );
}
