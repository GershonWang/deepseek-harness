/**
 * Type definitions for the dsh-doctor diagnostic and repair framework.
 * @module @deepseek-ai/dsh-doctor
 */

/** Severity level of a diagnostic finding. */
export type Severity = 'info' | 'warning' | 'error' | 'fatal'

/**
 * Repair effort level. Higher levels authorize more invasive changes.
 * - `1`: safe, reversible fixes (e.g. permission tweaks, harmless config additions)
 * - `2`: moderate fixes (e.g. regenerating derived data, resetting non-critical settings)
 * - `3`: destructive fixes (e.g. deleting corrupted state, reinstalling components)
 */
export type RepairLevel = 1 | 2 | 3

/** Category of a diagnostic check. */
export type CheckCategory = 'env' | 'config' | 'data' | 'plugin'

/**
 * Result of running a single diagnostic check.
 */
export interface CheckResult {
  /** Whether the check passed. */
  ok: boolean
  /** Human-readable summary of the result. */
  message: string
  /** Optional detailed explanation or remediation hints. */
  detail?: string
  /** Whether an automated repair is available for a failing check. */
  fixable: boolean
  /**
   * Suggested repair level required to fix this issue.
   * Only meaningful when `fixable` is `true`.
   */
  suggestedLevel: RepairLevel
}

/**
 * Result of applying a repair.
 */
export interface FixResult {
  /** Whether the repair succeeded. */
  ok: boolean
  /** Human-readable summary of what was done. */
  message: string
  /**
   * Path to a backup of the original state, when applicable.
   * Relative to the backup directory created for the repair run.
   */
  backupPath?: string
}

/**
 * A registered diagnostic check with an optional repair action.
 */
export interface DoctorCheck {
  /** Stable unique identifier for this check. */
  id: string
  /** Human-readable display name. */
  name: string
  /** Broad category grouping for this check. */
  category: CheckCategory
  /** Severity level applied when this check fails. */
  severity: Severity
  /**
   * Run the diagnostic check against the given harness home.
   * @param dshHome - absolute path to the harness home directory.
   * @returns the check result.
   */
  check(dshHome: string): Promise<CheckResult>
  /**
   * Attempt to repair the issue this check detects.
   * Only called when the check returns `ok: false` with `fixable: true`
   * and the requested repair level is at least `suggestedLevel`.
   * @param dshHome - absolute path to the harness home directory.
   * @param backupDir - absolute path to the backup directory for this repair run.
   * @returns the repair result.
   */
  fix?(dshHome: string, backupDir: string): Promise<FixResult>
}

/** Per-check entry in a diagnosis report. */
export interface DoctorReportCheckEntry {
  /** Check identifier. */
  id: string
  /** Check display name. */
  name: string
  /** Check category. */
  category: CheckCategory
  /** Check severity when failing. */
  severity: Severity
  /** Result of running the check. */
  result: CheckResult
}

/** Summary counters for a diagnosis report. */
export interface DoctorReportSummary {
  /** Total number of checks run. */
  total: number
  /** Number of passing checks. */
  ok: number
  /** Number of failing checks (excluding fatal). */
  failed: number
  /** Number of failing checks with fatal severity. */
  fatal: number
  /** Number of failing checks that have an available repair. */
  fixable: number
}

/**
 * Complete diagnosis report produced by {@link runDiagnosis}.
 */
export interface DoctorReport {
  /** Absolute path of the harness home that was diagnosed. */
  dshHome: string
  /** ISO timestamp of when the diagnosis was generated. */
  generatedAt: string
  /** Per-check results in registration order. */
  checks: DoctorReportCheckEntry[]
  /** Aggregate summary counters. */
  summary: DoctorReportSummary
}

/** Entry for a successfully applied repair. */
export interface RepairAppliedEntry {
  /** Identifier of the check whose repair was applied. */
  checkId: string
  /** Human-readable message from the repair. */
  message: string
}

/** Entry for a repair that was skipped. */
export interface RepairSkippedEntry {
  /** Identifier of the check whose repair was skipped. */
  checkId: string
  /** Reason why the repair was not applied. */
  reason: string
}

/**
 * Complete repair report produced by {@link runRepair}.
 */
export interface RepairReport {
  /** The repair level that was requested and applied. */
  level: RepairLevel
  /** Paths to backup directories created during this repair run. */
  backups: string[]
  /** Repairs that were successfully applied. */
  applied: RepairAppliedEntry[]
  /** Repairs that were skipped and why. */
  skipped: RepairSkippedEntry[]
}
