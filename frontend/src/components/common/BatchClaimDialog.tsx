import { useEffect, useState } from 'react';
import { UiButton } from './UiButton';
import type { SelectedFault } from './BatchClaimBar';

// BatchClaimDialog collects the single claim reason shared by every selected
// 故障事件. Submission is disabled until the reason passes the backend length
// rule so operators learn the constraint before the request is rejected.
export function BatchClaimDialog({ open, selected, submitting, onCancel, onSubmit }: {
  open: boolean;
  selected: Map<string, SelectedFault>;
  submitting: boolean;
  onCancel: () => void;
  onSubmit: (reason: string) => void;
}) {
  const [reason, setReason] = useState('');
  useEffect(() => { if (open) setReason(''); }, [open]);
  if (!open) return null;
  const codes = Array.from(selected.keys());
  const length = reason.trim().length;
  const valid = length >= 3 && length <= 500;
  return <div className="modal-backdrop">
    <section className="modal modal--wide" role="dialog" aria-modal="true" aria-label="批量认领故障">
      <h2>批量认领故障（{codes.length} 条）</h2>
      <p className="modal__hint">仅仍为「待认领(open)」的故障可被提交；任一条编号不存在、状态已变化或版本过期，整批都会被拒绝，不会产生部分更新。</p>
      <div className="claim-codes" aria-label="已勾选编号">
        {codes.map((code) => <code key={code}>{code}</code>)}
      </div>
      <label className="claim-reason">
        <span>认领原因 <small>{length}/500（至少 3 个字符）</small></span>
        <textarea value={reason} maxLength={500} rows={4} placeholder="例如：已与场站值班确认，当班统一认领并安排现场核查"
          onChange={(event) => setReason(event.target.value)} />
      </label>
      <footer>
        <button className="link-button" onClick={onCancel} disabled={submitting}>取消</button>
        <UiButton onClick={() => onSubmit(reason.trim())} disabled={!valid || submitting}>{submitting ? '提交中…' : '确认批量认领'}</UiButton>
      </footer>
    </section>
  </div>;
}
