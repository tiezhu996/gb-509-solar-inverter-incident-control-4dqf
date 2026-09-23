import type { BatchClaimItemFailure, BatchClaimResult } from '../../types/domain';

const REASON_LABELS: Record<string, string> = {
  duplicate_code: '编号重复',
  code_not_found: '编号不存在',
  not_pending_claim: '异常状态',
  version_conflict: '版本过期',
};

export function reasonLabel(reason: string): string {
  return REASON_LABELS[reason] || reason;
}

// BatchClaimResultPanel renders the success outcome (fault claims + 逆变器
// 联动) or the per-item 阻断明细 returned by the rejected transaction.
export function BatchClaimResultPanel({ result, rejection, onDismiss }: {
  result: BatchClaimResult | null;
  rejection: { detail: string; items: BatchClaimItemFailure[] } | null;
  onDismiss: () => void;
}) {
  if (!result && !rejection) return null;
  if (rejection) {
    return <section className="claim-result claim-result--blocked" role="alert">
      <header>
        <strong>整批已阻断，未执行任何更新</strong>
        <button className="link-button" onClick={onDismiss}>关闭</button>
      </header>
      <p>{rejection.detail}</p>
      <table>
        <thead><tr><th>编号</th><th>阻断原因</th><th>说明</th></tr></thead>
        <tbody>
          {rejection.items.map((item, index) => <tr key={`${item.code}-${index}`}>
            <td><strong>{item.code || '(空)'}</strong></td>
            <td><span className="block-reason">{reasonLabel(item.reason)}</span></td>
            <td>{item.detail}</td>
          </tr>)}
        </tbody>
      </table>
    </section>;
  }
  return <section className="claim-result claim-result--success" role="status">
    <header>
      <strong>批量认领成功</strong>
      <button className="link-button" onClick={onDismiss}>关闭</button>
    </header>
    <p>已认领 {result!.claimedFaults.length} 条故障；同一场站 {result!.cascadeUpdates.length} 台跳闸逆变器联动转为告警。所有变化均已写入审计。</p>
    <table>
      <thead><tr><th>编号</th><th>对象</th><th>场站</th><th>变化</th><th>版本</th></tr></thead>
      <tbody>
        {result!.claimedFaults.map((change) => <tr key={`fault-${change.id}`}>
          <td><strong>{change.code}</strong></td><td>故障事件</td><td>{change.facility}</td>
          <td>{change.beforeStatus} → {change.afterStatus}</td>
          <td>v{change.beforeVersion} → v{change.afterVersion}</td>
        </tr>)}
        {result!.cascadeUpdates.map((change) => <tr key={`inverter-${change.id}`}>
          <td><strong>{change.code}</strong></td><td>逆变器联动</td><td>{change.facility}</td>
          <td>{change.beforeStatus} → {change.afterStatus}</td>
          <td>v{change.beforeVersion} → v{change.afterVersion}</td>
        </tr>)}
      </tbody>
    </table>
  </section>;
}
