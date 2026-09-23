
export interface DomainRecord {
  id: number;
  code: string;
  name: string;
  status: string;
  version: number;
  description: string;
  facility: string;
  owner: string;
  category: string;
  riskLevel: 'low' | 'medium' | 'high' | 'critical';
  metricValue: number;
  metricUnit: string;
  effectiveAt: string;
  evidence: string;
  relatedCode: string;
  createdAt: string;
  updatedAt: string;
}

export interface PageMeta { page: number; pageSize: number; total: number }
export interface ApiEnvelope<T> { data: T; error?: string; message?: string; meta?: PageMeta | BatchClaimRejection }
export interface UserSession { token: string; username: string; displayName: string; role: string; expiresIn: number }

export interface BatchClaimFaultItem { code: string; expectedVersion: number }
export interface BatchClaimRequest { items: BatchClaimFaultItem[]; reason: string }
export interface BatchClaimFaultChange {
  id: number; code: string; facility: string;
  beforeStatus: string; afterStatus: string; beforeVersion: number; afterVersion: number;
}
export interface BatchClaimResult {
  claimedFaults: BatchClaimFaultChange[];
  cascadeUpdates: BatchClaimFaultChange[];
  reason: string;
}
export interface BatchClaimItemFailure { code: string; reason: string; detail: string }
export interface BatchClaimRejection { reason: string; detail: string; items: BatchClaimItemFailure[] }

export const BATCH_CLAIM_LIMIT = 20;
export const BATCH_CLAIM_PENDING_STATUS = 'open';
export interface AuditLog {
  id: number; requestId: string; actor: string; action: string; entityType: string;
  entityId: number; beforeState: string; afterState: string; detail: string; createdAt: string;
}
export interface EntityConfig { key: string; path: string; label: string; statuses: readonly string[]; batchClaim?: boolean }
