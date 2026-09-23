
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
export interface ApiEnvelope<T> { data: T; error?: string; message?: string; meta?: PageMeta }
export interface UserSession { token: string; username: string; displayName: string; role: string; expiresIn: number }
export interface AuditLog {
  id: number; requestId: string; actor: string; action: string; entityType: string;
  entityId: number; beforeState: string; afterState: string; detail: string; createdAt: string;
}
export interface EntityConfig { key: string; path: string; label: string; statuses: readonly string[] }

export interface BatchClaimItem { code: string; expectedVersion: number }
export interface BatchClaimBlock { code: string; reason: string }
export interface BatchClaimFault { id: number; code: string; version: number }
export interface BatchClaimInverterChange {
  id: number; code: string; facility: string; fromStatus: string; toStatus: string; version: number;
}
export interface BatchClaimResult {
  claimedFaults: BatchClaimFault[];
  inverterChanges: BatchClaimInverterChange[];
  affectedFacilities: string[];
  faultAuditCount: number;
  inverterAuditCount: number;
}
