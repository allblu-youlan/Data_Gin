package data_svc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/storage"
)

const (
	defaultExcelMatchCleanupBatchSize  = 100
	defaultExcelMatchCleanupMaxJobs    = 1000
	defaultExcelMatchCleanupStaleAfter = 20 * time.Minute
	excelMatchOSSDeleteTimeout         = 30 * time.Second
	excelMatchCleanupStateTimeout      = 5 * time.Second
)

type ExcelMatchCleanupResult struct {
	Scanned int
	Claimed int
	Expired int
	Deleted int
}

type excelMatchCleanupStore interface {
	ListExpiredCleanupCandidates(context.Context, time.Time, time.Time, uint, int) ([]model.ExcelMatchJob, error)
	ClaimExpiredCleanup(context.Context, model.ExcelMatchJob, time.Time, time.Time) (bool, error)
	FinishExpiredCleanup(context.Context, uint, string, int64) error
	ReleaseExpiredCleanup(context.Context, model.ExcelMatchJob, int64) error
}

type excelMatchCleanupObjectStore interface {
	DeleteObject(context.Context, string) error
}

type excelMatchCleanupObjectStoreFactory func() (excelMatchCleanupObjectStore, error)

type excelMatchCleanupLogger func(context.Context, uint, string, string, map[string]interface{})

type excelMatchJobCleaner struct {
	jobs            excelMatchCleanupStore
	newObjectStore  excelMatchCleanupObjectStoreFactory
	removeAll       func(string) error
	now             func() time.Time
	log             excelMatchCleanupLogger
	objectKeyPrefix string
	batchSize       int
	maxJobs         int
	staleAfter      time.Duration
	deleteTimeout   time.Duration
}

func newExcelMatchJobCleaner(jobDAO *data_dao.ExcelMatchJobDAO, log excelMatchCleanupLogger) *excelMatchJobCleaner {
	return &excelMatchJobCleaner{
		jobs: jobDAO,
		newObjectStore: func() (excelMatchCleanupObjectStore, error) {
			return storage.NewOSSClientFromConfig()
		},
		removeAll:       os.RemoveAll,
		now:             time.Now,
		log:             log,
		objectKeyPrefix: storage.BuildObjectKey("excel-match-results"),
		batchSize:       defaultExcelMatchCleanupBatchSize,
		maxJobs:         defaultExcelMatchCleanupMaxJobs,
		staleAfter:      defaultExcelMatchCleanupStaleAfter,
		deleteTimeout:   excelMatchOSSDeleteTimeout,
	}
}

func (cleaner *excelMatchJobCleaner) Cleanup(ctx context.Context) (ExcelMatchCleanupResult, error) {
	var result ExcelMatchCleanupResult
	if cleaner == nil || cleaner.jobs == nil || cleaner.newObjectStore == nil || cleaner.removeAll == nil ||
		cleaner.now == nil || cleaner.objectKeyPrefix == "" || ctx == nil || cleaner.batchSize < 1 || cleaner.batchSize > cleaner.maxJobs ||
		cleaner.maxJobs < 1 || cleaner.maxJobs > 5000 || cleaner.staleAfter <= 0 || cleaner.deleteTimeout <= 0 {
		return result, errors.New("excel match cleaner: invalid configuration")
	}

	var cleanupErrors []error
	var objectStore excelMatchCleanupObjectStore
	var objectStoreErr error
	objectStoreInitialized := false
	now := cleaner.now().UTC()
	var afterID uint
cleanupLoop:
	for result.Scanned < cleaner.maxJobs {
		limit := min(cleaner.batchSize, cleaner.maxJobs-result.Scanned)
		jobs, err := cleaner.jobs.ListExpiredCleanupCandidates(ctx, now, now.Add(-cleaner.staleAfter), afterID, limit)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("excel match cleaner: list expired jobs: %w", err))
			break
		}
		if len(jobs) == 0 {
			break
		}
		if len(jobs) > limit {
			cleanupErrors = append(cleanupErrors, errors.New("excel match cleaner: candidate batch exceeds limit"))
			break
		}
		for _, matchJob := range jobs {
			if matchJob.ID <= afterID {
				cleanupErrors = append(cleanupErrors, errors.New("excel match cleaner: invalid candidate cursor"))
				break cleanupLoop
			}
			afterID = matchJob.ID
			if err := ctx.Err(); err != nil {
				cleanupErrors = append(cleanupErrors, err)
				break cleanupLoop
			}
			result.Scanned++
			claimAt := cleaner.now().UTC()
			claimed, err := cleaner.jobs.ClaimExpiredCleanup(
				ctx,
				matchJob,
				claimAt,
				claimAt.Add(-cleaner.staleAfter),
			)
			if err != nil {
				cleaner.logError(ctx, matchJob.ID, "抢占过期任务清理租约失败", err)
				cleanupErrors = append(cleanupErrors, fmt.Errorf("excel match cleaner: job %d: claim cleanup: %w", matchJob.ID, err))
				continue
			}
			if !claimed {
				continue
			}
			result.Claimed++
			claimUnix := claimAt.Unix()
			objectKey := strings.TrimSpace(matchJob.ResultObjectKey)
			if objectKey != "" {
				if !validExcelMatchResultObjectKey(matchJob.ResultObjectKey, matchJob.ID, cleaner.objectKeyPrefix) {
					err := errors.New("excel match cleaner: invalid result object key")
					cleaner.logError(ctx, matchJob.ID, "OSS结果文件Key非法，已跳过删除", err)
					cleanupErrors = append(cleanupErrors, cleaner.releaseAfterError(ctx, matchJob, claimUnix, err))
					continue
				}
				if !objectStoreInitialized {
					objectStoreInitialized = true
					objectStore, objectStoreErr = cleaner.newObjectStore()
					if objectStoreErr == nil && objectStore == nil {
						objectStoreErr = errors.New("excel match cleaner: nil object store")
					}
				}
				if objectStoreErr != nil {
					cleaner.logError(ctx, matchJob.ID, "初始化OSS清理客户端失败，保留文件等待重试", objectStoreErr)
					cleanupErrors = append(cleanupErrors, cleaner.releaseAfterError(ctx, matchJob, claimUnix, objectStoreErr))
					continue
				}

				deleteCtx, cancel := context.WithTimeout(ctx, cleaner.deleteTimeout)
				deleteErr := objectStore.DeleteObject(deleteCtx, objectKey)
				cancel()
				if deleteErr != nil {
					cleaner.logError(ctx, matchJob.ID, "删除OSS结果文件失败，保留文件等待重试", deleteErr)
					cleanupErrors = append(cleanupErrors, cleaner.releaseAfterError(ctx, matchJob, claimUnix, deleteErr))
					continue
				}
				result.Deleted++
				cleaner.logJob(ctx, matchJob.ID, "info", "OSS过期结果文件已删除", map[string]interface{}{
					"object_key": objectKey,
				})
			}

			if matchJob.WorkDir != "" {
				if !isPathInside(excelMatchJobDir(matchJob.ID), matchJob.WorkDir) {
					cleaner.logJob(ctx, matchJob.ID, "warn", "任务目录路径非法，已跳过本地删除", nil)
				} else if err := cleaner.removeAll(matchJob.WorkDir); err != nil {
					cleaner.logError(ctx, matchJob.ID, "删除本地过期任务目录失败，保留状态等待重试", err)
					cleanupErrors = append(cleanupErrors, cleaner.releaseAfterError(ctx, matchJob, claimUnix, err))
					continue
				}
			}

			if err := cleaner.jobs.FinishExpiredCleanup(ctx, matchJob.ID, matchJob.ResultObjectKey, claimUnix); err != nil {
				cleaner.logError(ctx, matchJob.ID, "更新过期任务清理状态失败，等待幂等重试", err)
				cleanupErrors = append(cleanupErrors, fmt.Errorf("excel match cleaner: job %d: finish cleanup: %w", matchJob.ID, err))
				continue
			}
			result.Expired++
			cleaner.logJob(ctx, matchJob.ID, "info", "过期任务清理完成", nil)
		}
		if len(jobs) < limit {
			break
		}
	}

	return result, errors.Join(cleanupErrors...)
}

func (cleaner *excelMatchJobCleaner) releaseAfterError(
	ctx context.Context,
	candidate model.ExcelMatchJob,
	claimUnix int64,
	cause error,
) error {
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), excelMatchCleanupStateTimeout)
	defer cancel()
	releaseErr := cleaner.jobs.ReleaseExpiredCleanup(stateCtx, candidate, claimUnix)
	if errors.Is(releaseErr, data_dao.ErrExcelMatchCleanupLeaseLost) {
		releaseErr = nil
	}
	return errors.Join(cause, releaseErr)
}

func (cleaner *excelMatchJobCleaner) logError(ctx context.Context, jobID uint, message string, err error) {
	cleaner.logJob(ctx, jobID, "warn", message, map[string]interface{}{"error": err.Error()})
}

func (cleaner *excelMatchJobCleaner) logJob(
	ctx context.Context,
	jobID uint,
	level string,
	message string,
	detail map[string]interface{},
) {
	if cleaner.log != nil {
		cleaner.log(ctx, jobID, level, message, detail)
	}
}

func validExcelMatchResultObjectKey(value string, expectedJobID uint, expectedPrefix string) bool {
	if value == "" || len(value) > 1024 || value != strings.TrimSpace(value) || strings.HasPrefix(value, "/") ||
		strings.Contains(value, "\\") || path.Clean(value) != value {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	expectedPrefix = strings.Trim(expectedPrefix, "/")
	if expectedPrefix == "" || !strings.HasPrefix(value, expectedPrefix+"/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, expectedPrefix+"/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	if len(parts) != 5 || (parts[4] != excelMatchResultFileName && parts[4] != "result.zip") {
		return false
	}
	objectDate, err := time.Parse("2006/01/02", strings.Join(parts[:3], "/"))
	if err != nil {
		return false
	}
	jobID, err := strconv.ParseUint(parts[3], 10, 64)
	return err == nil && jobID > 0 && jobID == uint64(expectedJobID) &&
		value == path.Join(expectedPrefix, objectDate.Format("2006/01/02"), parts[3], parts[4])
}
