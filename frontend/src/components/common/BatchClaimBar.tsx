import type { DomainRecord } from '../../types/domain';
import { BATCH_CLAIM_LIMIT, BATCH_CLAIM_PENDING_STATUS } from '../../types/domain';
import { UiButton } from './UiButton';

export interface SelectedFault { code: string; expectedVersion: number }

export function BatchClaimBar({ selected, items, onClear, onClaim }: {
  selected: Map<string, SelectedFault>;
  items: DomainRecord[];
  onClear: () => void;
  onClaim: () => void;
}) {
  const openCount = items.filter((item) => item.status === BATCH_CLAIM_PENDING_STATUS).length;
  const limitReached = selected.size >= BATCH_CLAIM_LIMIT;
  return <section className="batch-bar" aria-live="polite">
    <div>
      <strong>批量认领</strong>
      <span>已勾选 {selected.size} 条待认领故障（本页可认领 {openCount} 条，单次最多 {BATCH_CLAIM_LIMIT} 条）</span>
      {limitReached && <em className="batch-bar__limit">已达单次上限</em>}
    </div>
    <div className="batch-bar__actions">
      <button className="link-button" onClick={onClear} disabled={selected.size === 0}>清空勾选</button>
      <UiButton onClick={onClaim} disabled={selected.size === 0}>提交认领</UiButton>
    </div>
  </section>;
}
