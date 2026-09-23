
import { useEffect, useMemo, useRef, useState } from 'react';
import type { EntityConfig, DomainRecord, BatchClaimResult, BatchClaimRejection } from '../types/domain';
import type { EntityStore } from '../stores/factory';
import { nextStatus, formatDate } from '../utils/format';
import { StatusBadge } from './common/StatusBadge';
import { MetricCard } from './common/MetricCard';
import { ConfirmDialog } from './common/ConfirmDialog';
import { UiButton } from './common/UiButton';
import { SeverityTag } from './common/SeverityTag';
import { ActionDrawer } from './common/ActionDrawer';
import { BatchClaimBar, type SelectedFault } from './common/BatchClaimBar';
import { BatchClaimDialog } from './common/BatchClaimDialog';
import { BatchClaimResultPanel } from './common/BatchClaimResultPanel';
import { getSession, ApiError } from '../api/client';
import { batchClaimFaultEvents } from '../api/fault-event';
import { BATCH_CLAIM_LIMIT, BATCH_CLAIM_PENDING_STATUS } from '../types/domain';

export function EntityPage({ config, useStore }: { config: EntityConfig; useStore: EntityStore }) {
  const { items, meta, loading, error, load, createRecord, transition } = useStore();
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; status: string } | null>(null);
  const [remoteConfirm, setRemoteConfirm] = useState(false);
  const [selected, setSelected] = useState<Map<string, SelectedFault>>(new Map());
  const [claimOpen, setClaimOpen] = useState(false);
  const [claimSubmitting, setClaimSubmitting] = useState(false);
  const [claimResult, setClaimResult] = useState<BatchClaimResult | null>(null);
  const [claimRejection, setClaimRejection] = useState<BatchClaimRejection | null>(null);
  const [claimTransportError, setClaimTransportError] = useState('');
  const selectAllRef = useRef<HTMLInputElement>(null);
  const role = getSession()?.role || 'viewer';
  const canWrite = ['operator', 'reviewer', 'admin'].includes(role);
  const isRemoteAction = ['faultEvent', 'mitigationAction'].includes(config.key);
  const batchClaimEnabled = Boolean(config.batchClaim);
  useEffect(() => { void load(config.path); }, [config.path, load]);

  // After a refresh, drop selections whose rows left the page or moved out of
  // the 待认领 state, and refresh cached versions so stale ticks cannot submit.
  useEffect(() => {
    if (!batchClaimEnabled) return;
    setSelected((previous) => {
      const next = new Map<string, SelectedFault>();
      const byCode = new Map(items.map((item) => [item.code, item]));
      previous.forEach((value, code) => {
        const item = byCode.get(code);
        if (item && item.status === BATCH_CLAIM_PENDING_STATUS) {
          next.set(code, { code, expectedVersion: item.version });
        }
      });
      return next.size === previous.size ? previous : next;
    });
  }, [items, batchClaimEnabled]);

  const openItems = useMemo(() => items.filter((item) => batchClaimEnabled && item.status === BATCH_CLAIM_PENDING_STATUS), [items, batchClaimEnabled]);
  const allOpenSelected = openItems.length > 0 && openItems.every((item) => selected.has(item.code));
  useEffect(() => {
    if (selectAllRef.current) selectAllRef.current.indeterminate = !allOpenSelected && openItems.some((item) => selected.has(item.code));
  }, [allOpenSelected, openItems, selected]);

  const highRisk = useMemo(() => items.filter((item) => ['high', 'critical'].includes(item.riskLevel)).length, [items]);
  const createDemo = async () => {
    const now = Date.now();
    await createRecord(config.path, { code: `${config.key.toUpperCase()}-${now.toString().slice(-6)}`, name: `新增${config.label}`,
      description: '通过前端工作台创建的业务记录', facility: '默认作业区', owner: '现场操作员', category: '常规', riskLevel: 'medium',
      metricValue: 25, metricUnit: 'unit', effectiveAt: new Date().toISOString(), evidence: '已完成创建前检查', relatedCode: '' });
    setShowCreate(false);
  };
  const finalizeTransition = async () => {
    if (!pending) return;
    try { await transition(config.path, pending.item, pending.status); setPending(null); setRemoteConfirm(false); }
    catch { setRemoteConfirm(false); }
  };

  const toggleOne = (item: DomainRecord) => {
    setClaimResult(null); setClaimRejection(null); setClaimTransportError('');
    setSelected((previous) => {
      const next = new Map(previous);
      if (next.has(item.code)) next.delete(item.code);
      else if (next.size < BATCH_CLAIM_LIMIT) next.set(item.code, { code: item.code, expectedVersion: item.version });
      return next;
    });
  };
  const toggleAll = () => {
    setSelected((previous) => {
      if (allOpenSelected) return new Map();
      const next = new Map(previous);
      for (const item of openItems) {
        if (next.size >= BATCH_CLAIM_LIMIT) break;
        next.set(item.code, { code: item.code, expectedVersion: item.version });
      }
      return next;
    });
  };

  const submitBatchClaim = async (reason: string) => {
    setClaimSubmitting(true);
    setClaimTransportError('');
    try {
      const payload = { items: Array.from(selected.values()), reason };
      const response = await batchClaimFaultEvents(payload);
      setClaimResult(response.data);
      setClaimRejection(null);
      setSelected(new Map());
      setClaimOpen(false);
      await load(config.path, search);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'batch_rejected' && err.meta) {
        setClaimRejection(err.meta as BatchClaimRejection);
        setClaimResult(null);
        setClaimOpen(false);
      } else if (err instanceof ApiError && err.code === 'version_conflict') {
        setClaimRejection({ reason: 'version_conflict', detail: '提交期间故障被其他请求改动，事务已回滚，请刷新后重试', items: [] });
        setClaimOpen(false);
      } else {
        setClaimTransportError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setClaimSubmitting(false);
    }
  };

  const columnCount = 8 + (batchClaimEnabled ? 1 : 0);
  return <main className="workspace">
    <header className="page-header"><div><p className="eyebrow">业务工作台</p><h1>{config.label}</h1><p>统一管理{config.label}的状态、风险、证据与责任人。</p></div>{canWrite && <UiButton onClick={() => setShowCreate(true)}>新增{config.label}</UiButton>}</header>
    <section className="metrics"><MetricCard label="记录总数" value={meta.total} detail="当前筛选范围"/><MetricCard label="高风险" value={highRisk} detail="需要优先复核"/><MetricCard label="状态种类" value={new Set(items.map((item) => item.status)).size} detail="状态机覆盖"/></section>
    {['inverterUnit', 'faultEvent'].includes(config.key) && <SeverityTag records={items} />}
    <section className="toolbar"><input aria-label="搜索" placeholder={`搜索${config.label}编码或名称`} value={search} onChange={(event) => setSearch(event.target.value)} /><UiButton onClick={() => void load(config.path, search)}>查询</UiButton><button className="link-button" onClick={() => { setSearch(''); void load(config.path); }}>重置</button></section>
    {batchClaimEnabled && canWrite && <BatchClaimBar selected={selected} items={items} onClear={() => setSelected(new Map())} onClaim={() => { setClaimTransportError(''); setClaimOpen(true); }} />}
    {error && <div className="alert" role="alert">{error}</div>}
    {claimTransportError && <div className="alert" role="alert">批量认领请求失败：{claimTransportError}</div>}
    {(claimResult || claimRejection) && <BatchClaimResultPanel result={claimResult} rejection={claimRejection} onDismiss={() => { setClaimResult(null); setClaimRejection(null); }} />}
    <section className="table-shell" aria-busy={loading}><table><thead><tr>{batchClaimEnabled && <th className="col-check"><input ref={selectAllRef} type="checkbox" aria-label="全选本页待认领" checked={allOpenSelected} disabled={!canWrite || openItems.length === 0} onChange={toggleAll} /></th>}<th>编号</th><th>名称</th><th>状态</th><th>风险</th><th>责任人</th><th>指标</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => { const next = nextStatus(item.status, config.statuses); const isOpen = item.status === BATCH_CLAIM_PENDING_STATUS; const checkable = batchClaimEnabled && canWrite && isOpen; const checked = batchClaimEnabled && selected.has(item.code); const disabledByLimit = checkable && !checked && selected.size >= BATCH_CLAIM_LIMIT; return <tr key={item.id} className={checked ? 'row-selected' : undefined}>{batchClaimEnabled && <td className="col-check"><input type="checkbox" aria-label={`勾选 ${item.code}`} checked={checked} disabled={!checkable || disabledByLimit} onChange={() => toggleOne(item)} /></td>}<td><strong>{item.code}</strong></td><td>{item.name}<small>{item.facility}</small></td><td><StatusBadge status={item.status}/></td><td>{item.riskLevel}</td><td>{item.owner}</td><td>{item.metricValue} {item.metricUnit}</td><td>{formatDate(item.updatedAt)}</td><td>{next ? <button className="table-action" disabled={!canWrite} onClick={() => setPending({ item, status: next })}>{canWrite ? '推进至' : '无权限推进至'} {next}</button> : <span className="muted">流程结束</span>}</td></tr>; })}
      {!items.length && !loading && <tr><td colSpan={columnCount} className="empty">暂无记录</td></tr>}
    </tbody></table>{loading && <div className="loading">正在同步业务数据…</div>}</section>
    <ConfirmDialog open={showCreate} title={`新增${config.label}`} onCancel={() => setShowCreate(false)} onConfirm={() => { void createDemo().catch(() => undefined); }}><p>将创建一条包含完整责任人、风险和证据信息的演示记录。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending) && !remoteConfirm} title="确认状态迁移" onCancel={() => setPending(null)} onConfirm={() => { if (isRemoteAction) setRemoteConfirm(true); else void finalizeTransition(); }}><p>状态迁移会写入审计日志，且使用版本号避免并发覆盖。</p><strong>{pending?.item.status} → {pending?.status}</strong></ConfirmDialog>
    <ActionDrawer open={remoteConfirm} item={pending?.item || null} target={pending?.status || ''} onCancel={() => setRemoteConfirm(false)} onConfirm={() => void finalizeTransition()} />
    <BatchClaimDialog open={claimOpen} selected={selected} submitting={claimSubmitting} onCancel={() => setClaimOpen(false)} onSubmit={(reason) => void submitBatchClaim(reason)} />
  </main>;
}
