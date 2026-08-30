package backup_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"sync"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


type multiDBMockRunner struct {
	mu           sync.Mutex
	capturedCmd  []string
	capturedEnv  []string
	capturedIn   []byte
	dumpPayload  []byte
	dumpExitCode int
	restExitCode int
}

func (m *multiDBMockRunner) ExecStreamStdout(ctx context.Context, containerID string, cmd []string, env []string, stdout io.Writer) (int, error) {
	m.mu.Lock()
	m.capturedCmd = cmd
	m.capturedEnv = env
	payload := m.dumpPayload
	code := m.dumpExitCode
	m.mu.Unlock()

	if payload != nil {
		_, _ = stdout.Write(payload)
	}
	return code, nil
}

func (m *multiDBMockRunner) ExecStreamStdin(ctx context.Context, containerID string, cmd []string, env []string, stdin io.Reader) (int, error) {
	m.mu.Lock()
	m.capturedCmd = cmd
	m.capturedEnv = env
	code := m.restExitCode
	m.mu.Unlock()

	data, _ := io.ReadAll(stdin)
	m.mu.Lock()
	m.capturedIn = data
	m.mu.Unlock()

	return code, nil
}

func TestBuildDumpAndRestoreCommands(t *testing.T) {
	tests := []struct {
		name              string
		engine            backup.DatabaseEngine
		dbName            string
		user              string
		pass              string
		expectedDumpCmd   []string
		expectedDumpEnv   []string
		expectedRestCmd   []string
		expectedRestEnv   []string
		isPreCompressed   bool
	}{
		{
			name:            "Postgres default",
			engine:          backup.EnginePostgres17,
			dbName:          "mydb",
			user:            "pguser",
			pass:            "secret",
			expectedDumpCmd: []string{"pg_dump", "-U", "pguser", "mydb"},
			expectedDumpEnv: []string{"PGPASSWORD=secret"},
			expectedRestCmd: []string{"psql", "-U", "pguser", "-d", "mydb"},
			expectedRestEnv: []string{"PGPASSWORD=secret"},
			isPreCompressed: false,
		},
		{
			name:            "MySQL with password",
			engine:          backup.EngineMySQL84,
			dbName:          "app_db",
			user:            "root",
			pass:            "mysecret",
			expectedDumpCmd: []string{"mysqldump", "--single-transaction", "--quick", "-u", "root", "-pmysecret", "app_db"},
			expectedDumpEnv: []string{"MYSQL_PWD=mysecret"},
			expectedRestCmd: []string{"mysql", "-u", "root", "-pmysecret", "app_db"},
			expectedRestEnv: []string{"MYSQL_PWD=mysecret"},
			isPreCompressed: false,
		},
		{
			name:            "MariaDB without password",
			engine:          backup.EngineMariaDB114,
			dbName:          "maria_db",
			user:            "maria_user",
			pass:            "",
			expectedDumpCmd: []string{"mysqldump", "--single-transaction", "--quick", "-u", "maria_user", "maria_db"},
			expectedDumpEnv: nil,
			expectedRestCmd: []string{"mysql", "-u", "maria_user", "maria_db"},
			expectedRestEnv: nil,
			isPreCompressed: false,
		},
		{
			name:            "Redis with password",
			engine:          backup.EngineRedis74,
			dbName:          "",
			user:            "",
			pass:            "redispass",
			expectedDumpCmd: []string{"redis-cli", "--rdb", "-", "-a", "redispass"},
			expectedDumpEnv: []string{"REDISCLI_AUTH=redispass"},
			expectedRestCmd: []string{"redis-cli", "-a", "redispass", "--pipe"},
			expectedRestEnv: []string{"REDISCLI_AUTH=redispass"},
			isPreCompressed: false,
		},
		{
			name:            "MongoDB with auth",
			engine:          backup.EngineMongo70,
			dbName:          "analytics",
			user:            "admin",
			pass:            "mongosecret",
			expectedDumpCmd: []string{"mongodump", "--archive", "--gzip", "--uri", "mongodb://admin:mongosecret@127.0.0.1:27017/analytics?authSource=admin"},
			expectedDumpEnv: nil,
			expectedRestCmd: []string{"mongorestore", "--archive", "--gzip", "--uri", "mongodb://admin:mongosecret@127.0.0.1:27017/analytics?authSource=admin", "--drop"},
			expectedRestEnv: nil,
			isPreCompressed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bCfg := backup.BackupJobConfig{
				Engine:       tc.engine,
				DatabaseName: tc.dbName,
				Username:     tc.user,
				Password:     tc.pass,
			}
			dumpCmd, dumpEnv, dumpPreComp, err := backup.BuildDumpCommand(bCfg)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedDumpCmd, dumpCmd)
			assert.Equal(t, tc.expectedDumpEnv, dumpEnv)
			assert.Equal(t, tc.isPreCompressed, dumpPreComp)

			rCfg := backup.RestoreJobConfig{
				Engine:       tc.engine,
				DatabaseName: tc.dbName,
				Username:     tc.user,
				Password:     tc.pass,
			}
			restCmd, restEnv, restPreComp, err := backup.BuildRestoreCommand(rCfg)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedRestCmd, restCmd)
			assert.Equal(t, tc.expectedRestEnv, restEnv)
			assert.Equal(t, tc.isPreCompressed, restPreComp)
		})
	}
}

func TestMultiDB_PureStreamingE2E_Postgres(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{storage: make(map[string][]byte), storeData: true}

	rawDumpData := bytes.Repeat([]byte("INSERT INTO users VALUES (1, 'Alice');\n"), 500)
	runner := &multiDBMockRunner{dumpPayload: rawDumpData}

	// 1. Backup
	res, err := backup.ExecuteMultiDBBackup(ctx, runner, mockS3, backup.BackupJobConfig{
		BackupID:     "bk_pg1",
		ProjectSlug:  "test-proj",
		ServiceSlug:  "pg-svc",
		ContainerID:  "cnt_pg_1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "appdb",
		Username:     "pguser",
		Password:     "secret",
		S3Bucket:     "backups",
	})
	require.NoError(t, err)
	assert.Equal(t, "bk_pg1", res.BackupID)
	assert.Equal(t, int64(len(rawDumpData)), res.UncompressedBytes)
	assert.Greater(t, res.CompressedBytes, int64(0))

	// 2. Restore
	err = backup.ExecuteMultiDBRestore(ctx, runner, mockS3, backup.RestoreJobConfig{
		RestoreID:    "rst_pg1",
		ProjectSlug:  "test-proj",
		ServiceSlug:  "pg-svc",
		ContainerID:  "cnt_pg_1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "appdb",
		Username:     "pguser",
		Password:     "secret",
		S3Bucket:     "backups",
		S3Key:        res.S3Key,
	})
	require.NoError(t, err)
	assert.Equal(t, rawDumpData, runner.capturedIn)
}

func TestMultiDB_PureStreamingE2E_MySQL(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{storage: make(map[string][]byte), storeData: true}

	rawDumpData := bytes.Repeat([]byte("LOCK TABLES `orders` WRITE;\nINSERT INTO `orders` VALUES (1);\n"), 300)
	runner := &multiDBMockRunner{dumpPayload: rawDumpData}

	// 1. Backup
	res, err := backup.ExecuteMultiDBBackup(ctx, runner, mockS3, backup.BackupJobConfig{
		BackupID:     "bk_mysql1",
		ProjectSlug:  "ecom",
		ServiceSlug:  "mysql-db",
		ContainerID:  "cnt_mysql_1",
		Engine:       backup.EngineMySQL84,
		DatabaseName: "shop",
		Username:     "root",
		Password:     "dbpass",
		S3Bucket:     "backups",
	})
	require.NoError(t, err)
	assert.Equal(t, "bk_mysql1", res.BackupID)
	assert.Equal(t, int64(len(rawDumpData)), res.UncompressedBytes)

	// 2. Restore
	err = backup.ExecuteMultiDBRestore(ctx, runner, mockS3, backup.RestoreJobConfig{
		RestoreID:    "rst_mysql1",
		ProjectSlug:  "ecom",
		ServiceSlug:  "mysql-db",
		ContainerID:  "cnt_mysql_1",
		Engine:       backup.EngineMySQL84,
		DatabaseName: "shop",
		Username:     "root",
		Password:     "dbpass",
		S3Bucket:     "backups",
		S3Key:        res.S3Key,
	})
	require.NoError(t, err)
	assert.Equal(t, rawDumpData, runner.capturedIn)
}

func TestMultiDB_PureStreamingE2E_Redis(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{storage: make(map[string][]byte), storeData: true}

	// Simulated RDB binary snapshot header: REDIS0011...
	rawRDB := append([]byte("REDIS0011"), bytes.Repeat([]byte{0x00, 0xFE, 0x00, 0x01, 0x02, 0xFF}, 200)...)
	runner := &multiDBMockRunner{dumpPayload: rawRDB}

	// 1. Backup
	res, err := backup.ExecuteMultiDBBackup(ctx, runner, mockS3, backup.BackupJobConfig{
		BackupID:    "bk_redis1",
		ProjectSlug: "cache-proj",
		ServiceSlug: "redis-svc",
		ContainerID: "cnt_redis_1",
		Engine:      backup.EngineRedis74,
		Password:    "redisauth",
		S3Bucket:    "backups",
	})
	require.NoError(t, err)
	assert.Equal(t, "bk_redis1", res.BackupID)
	assert.Equal(t, int64(len(rawRDB)), res.UncompressedBytes)

	// 2. Restore
	err = backup.ExecuteMultiDBRestore(ctx, runner, mockS3, backup.RestoreJobConfig{
		RestoreID:   "rst_redis1",
		ProjectSlug: "cache-proj",
		ServiceSlug: "redis-svc",
		ContainerID: "cnt_redis_1",
		Engine:      backup.EngineRedis74,
		Password:    "redisauth",
		S3Bucket:    "backups",
		S3Key:       res.S3Key,
	})
	require.NoError(t, err)
	assert.Equal(t, rawRDB, runner.capturedIn)
}

func TestMultiDB_PureStreamingE2E_Mongo(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{storage: make(map[string][]byte), storeData: true}

	// Simulated mongodump --archive --gzip payload (already gzipped stream)
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(bytes.Repeat([]byte("BSON_DUMP_DOCUMENTS_STREAM\n"), 300))
	_ = gw.Close()
	mongodumpArchiveGzip := gzBuf.Bytes()

	runner := &multiDBMockRunner{dumpPayload: mongodumpArchiveGzip}

	// 1. Backup
	res, err := backup.ExecuteMultiDBBackup(ctx, runner, mockS3, backup.BackupJobConfig{
		BackupID:     "bk_mongo1",
		ProjectSlug:  "doc-proj",
		ServiceSlug:  "mongo-db",
		ContainerID:  "cnt_mongo_1",
		Engine:       backup.EngineMongo70,
		DatabaseName: "analytics",
		Username:     "admin",
		Password:     "pass123",
		S3Bucket:     "backups",
	})
	require.NoError(t, err)
	assert.Equal(t, "bk_mongo1", res.BackupID)

	// 2. Restore
	err = backup.ExecuteMultiDBRestore(ctx, runner, mockS3, backup.RestoreJobConfig{
		RestoreID:    "rst_mongo1",
		ProjectSlug:  "doc-proj",
		ServiceSlug:  "mongo-db",
		ContainerID:  "cnt_mongo_1",
		Engine:       backup.EngineMongo70,
		DatabaseName: "analytics",
		Username:     "admin",
		Password:     "pass123",
		S3Bucket:     "backups",
		S3Key:        res.S3Key,
	})
	require.NoError(t, err)
	assert.Equal(t, mongodumpArchiveGzip, runner.capturedIn)
}

func TestMultiDB_UnsupportedEngineError(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{}
	runner := &multiDBMockRunner{}

	_, err := backup.ExecuteMultiDBBackup(ctx, runner, mockS3, backup.BackupJobConfig{
		Engine: "cassandra:4.0",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported database engine")

	err = backup.ExecuteMultiDBRestore(ctx, runner, mockS3, backup.RestoreJobConfig{
		Engine: "cassandra:4.0",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported database engine")
}

func TestMultiDB_CustomS3ClientResolution(t *testing.T) {
	ctx := context.Background()
	globalS3 := &mockS3MultipartClient{storage: make(map[string][]byte), storeData: true}
	customS3 := &mockS3MultipartClient{storage: make(map[string][]byte), storeData: true}

	rawDump := []byte("PG_CUSTOM_DUMP_CONTENT")
	runner := &multiDBMockRunner{dumpPayload: rawDump}

	// 1. Backup using custom S3Client explicitly injected via BackupJobConfig.S3Client
	res, err := backup.ExecuteMultiDBBackup(ctx, runner, globalS3, backup.BackupJobConfig{
		BackupID:     "bk_custom_s3",
		ProjectSlug:  "tenant-proj",
		ServiceSlug:  "custom-db",
		ContainerID:  "cnt_custom_1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "custom_db",
		S3Bucket:     "custom-bucket",
		S3Client:     customS3,
	})
	require.NoError(t, err)
	assert.Equal(t, "bk_custom_s3", res.BackupID)

	// Verify customS3 received the upload, not globalS3
	assert.Empty(t, globalS3.storage)
	assert.NotEmpty(t, customS3.storage)
	assert.Contains(t, customS3.storage, res.S3Key)

	// 2. Restore using custom S3Client
	err = backup.ExecuteMultiDBRestore(ctx, runner, globalS3, backup.RestoreJobConfig{
		RestoreID:    "rst_custom_s3",
		ProjectSlug:  "tenant-proj",
		ServiceSlug:  "custom-db",
		ContainerID:  "cnt_custom_1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "custom_db",
		S3Bucket:     "custom-bucket",
		S3Key:        res.S3Key,
		S3Client:     customS3,
	})
	require.NoError(t, err)
	assert.Equal(t, rawDump, runner.capturedIn)
}

