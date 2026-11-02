import {it,expect} from "vitest";
import {renderToStaticMarkup} from "react-dom/server";
import {StorageRecoveryMessage, recoverySignature} from "./StorageRecoveryNotice";
import {fromStorageRecoveryStatus} from "./wire";

it("shows recovery without exposing quarantined metadata or blocking healthy records",()=>{
  const status=fromStorageRecoveryStatus({quarantinedFavorites:1,quarantinedHistory:2,settingsRecovered:true,rawMetadata:"private"});
  const html=renderToStaticMarkup(<StorageRecoveryMessage status={status}/>);
  expect(html).toContain("恢复默认设置");expect(html).toContain("3 条");expect(html).toContain("隔离保留");expect(html).not.toContain("private");
});
it("does not show a recovery warning for healthy data",()=>{
  expect(renderToStaticMarkup(<StorageRecoveryMessage status={{quarantinedFavorites:0,quarantinedHistory:0,settingsRecovered:false}}/>)).toBe("");
  expect(()=>fromStorageRecoveryStatus({quarantinedFavorites:-1,quarantinedHistory:0,settingsRecovered:false})).toThrow();
});

it("dismisses an incident for this session while allowing changed cumulative counts or settings recovery", () => {
  const status = { quarantinedFavorites: 1, quarantinedHistory: 2, settingsRecovered: false };
  const dismissed = new Set([recoverySignature(status)]);
  const render = (next: typeof status) => renderToStaticMarkup(<StorageRecoveryMessage status={next} dismissed={dismissed} onDismiss={() => {}}/>);
  expect(render({ ...status })).toBe("");
  expect(render({ ...status, quarantinedFavorites: 2 })).toContain("知道了");
  // Moving a count between categories must also identify a different incident.
  expect(render({ ...status, quarantinedFavorites: 2, quarantinedHistory: 1 })).toContain("隔离保留");
  expect(render({ ...status, settingsRecovered: true })).toContain("恢复默认设置");
  expect(render({ ...status })).toBe("");
  const html = renderToStaticMarkup(<StorageRecoveryMessage status={status} onDismiss={() => {}}/>);
  expect(html).toContain('role="status"');
  expect(html).toContain("本次会话");
  expect(html).not.toContain('class="error');
});
