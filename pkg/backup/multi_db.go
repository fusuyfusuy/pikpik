package backup

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup/s3"
)


// EngineKind classifies database types for pipeline dispatch.
type EngineKind string

const (
	EngineKindPostgres EngineKind = "postgres"
	EngineKindMySQL    EngineKind = "mysql"
	EngineKindMariaDB  EngineKind = "mariadb"
	EngineKindRedis    EngineKind = "redis"
	EngineKindMongo    EngineKind = "mongo"
)

// DetectEngineKind parses a DatabaseEngine string into its EngineKind classification.
func DetectEngineKind(engine DatabaseEngine) (EngineKind, error) {
	s := strings.ToLower(strings.TrimSpace(string(engine)))
	switch {
	case strings.HasPrefix(s, "postgres") || strings.HasPrefix(s, "pg") || s == "":
		return EngineKindPostgres, nil
	case strings.HasPrefix(s, "mysql"):
		return EngineKindMySQL, nil
	case strings.HasPrefix(s, "maria") || strings.HasPrefix(s, "mariadb"):
		return EngineKindMariaDB, nil
	case strings.HasPrefix(s, "redis"):
		return EngineKindRedis, nil
	case strings.HasPrefix(s, "mongo"):
		return EngineKindMongo, nil
	default:
		return "", fmt.Errorf("unsupported database engine: %q", engine)
	}
}

// BuildMongoURI constructs a local MongoDB connection URI from credentials.
func BuildMongoURI(username, password, databaseName string) string {
	dbName := strings.TrimSpace(databaseName)
	if strings.HasPrefix(dbName, "mongodb://") || strings.HasPrefix(dbName, "mongodb+srv://") {
		return dbName
	}

	user := strings.TrimSpace(username)
	pass := password

	if user != "" && pass != "" {
		if dbName != "" {
			return fmt.Sprintf("mongodb://%s:%s@127.0.0.1:27017/%s?authSource=admin", user, pass, dbName)
		}
		return fmt.Sprintf("mongodb://%s:%s@127.0.0.1:27017/?authSource=admin", user, pass)
	} else if user != "" {
		if dbName != "" {
			return fmt.Sprintf("mongodb://%s@127.0.0.1:27017/%s", user, dbName)
		}
		return fmt.Sprintf("mongodb://%s@127.0.0.1:27017/", user)
	}

	if dbName != "" {
		return fmt.Sprintf("mongodb://127.0.0.1:27017/%s", dbName)
	}
	return "mongodb://127.0.0.1:27017"
}

// BuildDumpCommand builds the command, environment variables, and pre-compressed indicator for a backup job.
func BuildDumpCommand(cfg BackupJobConfig) (cmd []string, env []string, isPreCompressed bool, err error) {
	kind, err := DetectEngineKind(cfg.Engine)
	if err != nil {
		return nil, nil, false, err
	}

	switch kind {
	case EngineKindPostgres:
		dbName := cfg.DatabaseName
		if dbName == "" {
			dbName = "postgres"
		}
		user := cfg.Username
		if user == "" {
			user = "pguser"
		}
		cmd = []string{"pg_dump", "-U", user, dbName}
		if cfg.Password != "" {
			env = append(env, "PGPASSWORD="+cfg.Password)
		}
		return cmd, env, false, nil

	case EngineKindMySQL, EngineKindMariaDB:
		dbName := cfg.DatabaseName
		if dbName == "" {
			dbName = "mysql"
		}
		user := cfg.Username
		if user == "" {
			user = "root"
		}
		if cfg.Password != "" {
			cmd = []string{"mysqldump", "--single-transaction", "--quick", "-u", user, "-p" + cfg.Password, dbName}
			env = append(env, "MYSQL_PWD="+cfg.Password)
		} else {
			cmd = []string{"mysqldump", "--single-transaction", "--quick", "-u", user, dbName}
		}
		return cmd, env, false, nil

	case EngineKindRedis:
		if cfg.Password != "" {
			cmd = []string{"redis-cli", "--rdb", "-", "-a", cfg.Password}
			env = append(env, "REDISCLI_AUTH="+cfg.Password)
		} else {
			cmd = []string{"redis-cli", "--rdb", "-"}
		}
		return cmd, env, false, nil

	case EngineKindMongo:
		uri := BuildMongoURI(cfg.Username, cfg.Password, cfg.DatabaseName)
		cmd = []string{"mongodump", "--archive", "--gzip", "--uri", uri}
		return cmd, env, true, nil

	default:
		return nil, nil, false, fmt.Errorf("unknown engine kind: %s", kind)
	}
}

// BuildRestoreCommand builds the command, environment variables, and pre-compressed indicator for a restore job.
func BuildRestoreCommand(cfg RestoreJobConfig) (cmd []string, env []string, isPreCompressed bool, err error) {
	kind, err := DetectEngineKind(cfg.Engine)
	if err != nil {
		return nil, nil, false, err
	}

	switch kind {
	case EngineKindPostgres:
		dbName := cfg.DatabaseName
		if dbName == "" {
			dbName = "postgres"
		}
		user := cfg.Username
		if user == "" {
			user = "pguser"
		}
		cmd = []string{"psql", "-U", user, "-d", dbName}
		if cfg.Password != "" {
			env = append(env, "PGPASSWORD="+cfg.Password)
		}
		return cmd, env, false, nil

	case EngineKindMySQL, EngineKindMariaDB:
		dbName := cfg.DatabaseName
		if dbName == "" {
			dbName = "mysql"
		}
		user := cfg.Username
		if user == "" {
			user = "root"
		}
		if cfg.Password != "" {
			cmd = []string{"mysql", "-u", user, "-p" + cfg.Password, dbName}
			env = append(env, "MYSQL_PWD="+cfg.Password)
		} else {
			cmd = []string{"mysql", "-u", user, dbName}
		}
		return cmd, env, false, nil

	case EngineKindRedis:
		if cfg.Password != "" {
			cmd = []string{"redis-cli", "-a", cfg.Password, "--pipe"}
			env = append(env, "REDISCLI_AUTH="+cfg.Password)
		} else {
			cmd = []string{"redis-cli", "--pipe"}
		}
		return cmd, env, false, nil

	case EngineKindMongo:
		uri := BuildMongoURI(cfg.Username, cfg.Password, cfg.DatabaseName)
		cmd = []string{"mongorestore", "--archive", "--gzip", "--uri", uri, "--drop"}
		return cmd, env, true, nil

	default:
		return nil, nil, false, fmt.Errorf("unknown engine kind: %s", kind)
	}
}

// resolveS3Client determines the appropriate S3Client based on per-schedule options or default client.
func resolveS3Client(defaultClient s3.S3Client, bucket, endpoint, region, accessKey, secretKey string, customClient s3.S3Client) (s3.S3Client, error) {
	if customClient != nil {
		return customClient, nil
	}
	if accessKey != "" || endpoint != "" || secretKey != "" || (defaultClient == nil && bucket != "") {
		opts := s3.ClientOptions{
			Bucket:          bucket,
			Endpoint:        endpoint,
			Region:          region,
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
		}
		if strings.Contains(endpoint, "r2.cloudflarestorage.com") {
			opts.Provider = s3.ProviderR2
		} else if strings.Contains(endpoint, "backblazeb2.com") {
			opts.Provider = s3.ProviderBackblaze
		} else if endpoint != "" {
			opts.Provider = s3.ProviderMinIO
			opts.ForcePathStyle = true
		} else {
			opts.Provider = s3.ProviderAWS
		}
		return s3.NewClient(opts)
	}
	if defaultClient != nil {
		return defaultClient, nil
	}
	return nil, errors.New("s3 client is not configured")
}

// ExecuteMultiDBBackup coordinates a zero-disk memory-bounded streaming backup to S3.
func ExecuteMultiDBBackup(ctx context.Context, exec DockerExecRunner, s3Client s3.S3Client, cfg BackupJobConfig) (*BackupResult, error) {
	targetS3, err := resolveS3Client(s3Client, cfg.S3Bucket, cfg.S3Endpoint, cfg.S3Region, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Client)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	nowUTC := startTime.UTC()

	backupID := cfg.BackupID
	if backupID == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		backupID = "bk_" + hex.EncodeToString(b)
	}

	projectSlug := cfg.ProjectSlug
	if projectSlug == "" {
		projectSlug = "default"
	}
	serviceSlug := cfg.ServiceSlug
	if serviceSlug == "" {
		serviceSlug = "database"
	}

	// 1. Build Dump Command & Env
	cmd, env, isPreCompressed, err := BuildDumpCommand(cfg)
	if err != nil {
		return nil, err
	}

	// 2. Build S3 Key
	engineSlug := strings.ReplaceAll(string(cfg.Engine), ":", "")
	if engineSlug == "" {
		engineSlug = "postgres17"
	}
	tsStr := nowUTC.Format("2006-01-02T15-04-05Z")
	s3Key := fmt.Sprintf("backups/%s/%s/%s_%s_%s.dump.gz", projectSlug, serviceSlug, tsStr, engineSlug, backupID)

	// 3. Pipe & Stream Setup
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	uncompressedCounter := &countingWriter{w: io.Discard}
	var execExitCode int
	var execErr error

	execDone := make(chan struct{})
	go func() {
		defer close(execDone)
		defer pipeWriter.Close()

		var execWriter io.Writer
		var gw *gzip.Writer

		if isPreCompressed {
			// Pre-compressed streams (e.g. mongodump --archive --gzip) write directly to counter and pipe
			execWriter = io.MultiWriter(uncompressedCounter, pipeWriter)
		} else {
			// Uncompressed stdout (pg_dump, mysqldump, redis-cli) is piped through gzip.Writer
			gw = gzip.NewWriter(pipeWriter)
			execWriter = io.MultiWriter(uncompressedCounter, gw)
		}

		exitCode, runErr := exec.ExecStreamStdout(execCtx, cfg.ContainerID, cmd, env, execWriter)
		execExitCode = exitCode
		execErr = runErr

		if gw != nil {
			if closeErr := gw.Close(); closeErr != nil && runErr == nil {
				execErr = closeErr
			}
		}

		if runErr != nil {
			_ = pipeWriter.CloseWithError(runErr)
			return
		}
		if exitCode != 0 {
			_ = pipeWriter.CloseWithError(fmt.Errorf("%w: exit code %d", ErrContainerExecFailed, exitCode))
			return
		}
	}()

	// 4. Stream upload directly to S3
	uploadOpts := s3.UploadOptions{
		ContentType: "application/gzip",
		Metadata: map[string]string{
			"pikpik-backup-id":  backupID,
			"pikpik-project":    projectSlug,
			"pikpik-service":    serviceSlug,
			"pikpik-engine":     string(cfg.Engine),
			"pikpik-created-at": nowUTC.Format(time.RFC3339),
		},
	}

	objInfo, uploadErr := targetS3.UploadStreamMultipart(ctx, s3Key, pipeReader, uploadOpts)
	if uploadErr != nil {
		execCancel()
		_ = pipeReader.CloseWithError(uploadErr)
	}
	<-execDone

	if execErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrStreamingPipeFailed, execErr)
	}
	if execExitCode != 0 {
		return nil, fmt.Errorf("%w: container dump returned exit code %d", ErrContainerExecFailed, execExitCode)
	}
	if uploadErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrS3UploadAborted, uploadErr)
	}

	// 5. Grandfather-Father-Son Retention Pruning
	if cfg.RetentionRules.KeepHourly > 0 || cfg.RetentionRules.KeepDaily > 0 || cfg.RetentionRules.MaxBackups > 0 {
		prefix := fmt.Sprintf("backups/%s/%s/", projectSlug, serviceSlug)
		_, _ = targetS3.PruneRetention(ctx, prefix, s3.RetentionPolicy{
			KeepHourly:  cfg.RetentionRules.KeepHourly,
			KeepDaily:   cfg.RetentionRules.KeepDaily,
			KeepWeekly:  cfg.RetentionRules.KeepWeekly,
			KeepMonthly: cfg.RetentionRules.KeepMonthly,
			MaxBackups:  cfg.RetentionRules.MaxBackups,
		})
	}

	duration := time.Since(startTime)
	return &BackupResult{
		BackupID:          backupID,
		S3Key:             s3Key,
		ETag:              objInfo.ETag,
		CompressedBytes:   objInfo.Size,
		UncompressedBytes: uncompressedCounter.BytesWritten(),
		DurationMs:        duration.Milliseconds(),
		CreatedAt:         nowUTC,
		Engine:            cfg.Engine,
	}, nil
}

// ExecuteMultiDBRestore downloads an S3 backup stream, decompresses it in memory (if needed), and pipes to container stdin.
func ExecuteMultiDBRestore(ctx context.Context, exec DockerExecRunner, s3Client s3.S3Client, cfg RestoreJobConfig) error {
	targetS3, err := resolveS3Client(s3Client, cfg.S3Bucket, cfg.S3Endpoint, cfg.S3Region, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Client)
	if err != nil {
		return err
	}

	cmd, env, isPreCompressed, err := BuildRestoreCommand(cfg)
	if err != nil {
		return err
	}

	// 1. Download stream directly from S3
	body, _, err := targetS3.DownloadStream(ctx, cfg.S3Key)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrS3ObjectNotFound, err)
	}
	defer body.Close()

	var stdinStream io.Reader
	if isPreCompressed {
		// e.g. mongorestore with --gzip accepts compressed stream directly
		stdinStream = body
	} else {
		// Decompress stream in memory
		gzReader, err := gzip.NewReader(body)
		if err != nil {
			return fmt.Errorf("failed to initialize gzip decompressor stream: %w", err)
		}
		defer gzReader.Close()
		stdinStream = gzReader
	}

	// 2. Pipe decompressed stream into container stdin
	exitCode, err := exec.ExecStreamStdin(ctx, cfg.ContainerID, cmd, env, stdinStream)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRestoreStdinClosed, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("%w: container restore returned exit code %d", ErrContainerExecFailed, exitCode)
	}

	return nil
}
