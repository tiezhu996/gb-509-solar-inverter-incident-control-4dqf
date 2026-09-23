import { useEffect, useMemo, useState } from 'react';
import type { BatchClaimBlock, BatchClaimResult, DomainRecord } from '../../types/domain';
import { batchClaimFaults } from '../../api/fault-event';
import { ApiError } from '../../api/client';
import { UiButton } from './UiButton';
import { StatusBadge } from './StatusBadge';

const REASON_MIN = 3;
const REASON_MAX = 500;

export function BatchClaimDialog({
  open, items, onClose, onCommitted,
}: {
  open: boolean;
  items: DomainRecord[];
  onClose: () => void;
  onCommitted: () => Promise<void> | void;
}) {
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [blocks, setBlocks] = useState<BatchClaimBlock[]>([]);
  const [submitError, setSubmitError] = useState('');
  const [result, setResult] = useState<BatchClaimResult | null>(null);

  useEffect(() => {
    if (open) {
      setReason('');
      setSubmitting(false);
      setBlocks([]);
      setSubmitError('');
      setResult(null);
    }
  }, [open]);

  const reasonValid = reason.trim().length >= REASON_MIN && reason.trim().length <= REASON_MAX;
  const remaining = useMemo(() => REASON_MAX - reason.length, [reason]);

  if (!open) return null;

  const submit = async () => {
    if (!reasonValid || submitting) return;
    setSubmitting(true);
    setBlocks([]);
    setSubmitError('');
    try {
      const response = await batchClaimFaults(
        items.map((item) => ({ code: item.code, expectedVersion: item.version })),
        reason.trim(),
      );
      setResult(response.data);
      await onCommitted();
    } catch (error) {
      if (error instanceof ApiError && error.code === 'batch_claim_rejected') {
        const details = error.details as { blocks?: BatchClaimBlock[] } | undefined;
        setBlocks(details?.blocks || []);
        setSubmitError(error.message);
      } else {
        setSubmitError(error instanceof Error ? error.message : String(error));
      }
    } finally {
      setSubmitting(false);
    }
  };

  return <div className="modal-backdrop"><section className="modal modal--wide" role="dialog" aria-modal="true" aria-label="批量认领故障">
    <h2>{result ? '批量认领完成' : '批量认领故障'}</h2>

    {!result && <>
      <p className="batch-hint">将把勾选的 <strong>{items.length}</strong> 条待认领（open）故障一次性推进为已认领（acknowledged）；同一场站处于跳闸（tripped）的逆变器将自动转为告警（warning）。全部变化在同一事务内提交，任一编号被阻断则整批不变。</p>
      <ul className="batch-code-list">
        {items.map((item) => <li key={item.id}><strong>{item.code}</strong><span>{item.name}</span><small>{item.facility} · 版本 {item.version}</small></li>)}
      </ul>
      <label className="batch-reason">
        <span>认领原因（{reason.trim().length}/{REASON_MAX}，至少 {REASON_MIN} 字）</span>
        <textarea value={reason} maxLength={REASON_MAX} rows={3} placeholder="请填写本次批量认领的统一原因" onChange={(event) => setReason(event.target.value)} />
        <small className={remaining < 20 ? 'batch-reason__count batch-reason__count--low' : 'batch-reason__count'}>剩余 {remaining} 字</small>
      </label>

      {submitError && <div className="alert" role="alert">
        <strong>整批已被拒绝，未发生任何变更。</strong>{submitError}
        {blocks.length > 0 && <table className="block-table"><thead><tr><th>编号</th><th>阻断原因</th></tr></thead><tbody>
          {blocks.map((block, index) => <tr key={`${block.code}-${index}`}><td>{block.code || '—'}</td><td>{block.reason}</td></tr>)}
        </tbody></table>}
      </div>}

      <footer><button className="link-button" onClick={onClose} disabled={submitting}>取消</button>
        <UiButton onClick={() => void submit()} disabled={!reasonValid || submitting}>{submitting ? '提交中…' : `提交认领（${items.length} 条）`}</UiButton></footer>
    </>}

    {result && <>
      <div className="batch-result-summary">
        <span>认领故障 <strong>{result.faultAuditCount}</strong> 条</span>
        <span>联动逆变器 <strong>{result.inverterAuditCount}</strong> 台</span>
        <span>审计记录 <strong>{result.faultAuditCount + result.inverterAuditCount}</strong> 条</span>
      </div>
      <h3>已认领故障</h3>
      <ul className="batch-code-list">
        {result.claimedFaults.map((fault) => <li key={fault.id}><strong>{fault.code}</strong><span>open → acknowledged</span><small>新版本 {fault.version}</small></li>)}
      </ul>
      <h3>联动逆变器（tripped → warning）</h3>
      {result.inverterChanges.length === 0
        ? <p className="muted">受影响场站中没有处于跳闸状态的逆变器。</p>
        : <ul className="batch-code-list">
            {result.inverterChanges.map((inverter) => <li key={inverter.id}><strong>{inverter.code}</strong><span><StatusBadge status={inverter.fromStatus} /> → <StatusBadge status={inverter.toStatus} /></span><small>{inverter.facility} · 新版本 {inverter.version}</small></li>)}
          </ul>}
      <footer><UiButton onClick={onClose}>完成</UiButton></footer>
    </>}
  </section></div>;
}
