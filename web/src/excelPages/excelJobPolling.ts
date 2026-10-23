export function nextExcelJobPollState(consecutiveFailures: number, succeeded: boolean, failureLimit: number) {
  const failures = succeeded ? 0 : consecutiveFailures + 1
  return {
    consecutiveFailures: failures,
    stop: failures >= failureLimit,
    delayMilliseconds: succeeded ? 2_000 : Math.min(30_000, 2_000 * 2 ** Math.min(failures, 4)),
  }
}
