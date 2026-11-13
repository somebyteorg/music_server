package db_test

import (
	"context"
	"database/sql"
	"time"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Optimize", func() {
	var (
		ctx      context.Context
		database *sql.DB
		now      time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		now = time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)
		tests.InitDB(ctx)
		database = db.Db()
		_, err := database.Exec("TRUNCATE property")
		Expect(err).ToNot(HaveOccurred())
	})

	putProperty := func(key, value string) {
		_, err := database.Exec(`insert into property(id, value) values($1, $2)
			on conflict(id) do update set value=excluded.value`, key, value)
		Expect(err).ToNot(HaveOccurred())
	}

	getProperty := func(key string) string {
		var value string
		Expect(database.QueryRow("select value from property where id=$1", key).Scan(&value)).To(Succeed())
		return value
	}

	It("runs when no previous analysis was recorded", func() {
		ran, err := db.OptimizeDBIfNeeded(ctx, database, now)
		Expect(err).ToNot(HaveOccurred())
		Expect(ran).To(BeTrue())
		Expect(getProperty(consts.LastDBAnalyzeAtKey)).To(Equal(now.Format(time.RFC3339Nano)))
	})

	It("skips a recent analysis when no refresh is pending", func() {
		lastAnalyze := now.Add(-23 * time.Hour)
		putProperty(consts.LastDBAnalyzeAtKey, lastAnalyze.Format(time.RFC3339Nano))
		putProperty(consts.DBAnalyzePendingKey, "0")

		ran, err := db.OptimizeDBIfNeeded(ctx, database, now)
		Expect(err).ToNot(HaveOccurred())
		Expect(ran).To(BeFalse())
		Expect(getProperty(consts.LastDBAnalyzeAtKey)).To(Equal(lastAnalyze.Format(time.RFC3339Nano)))

	})

	It("runs when the previous analysis is stale", func() {
		putProperty(consts.LastDBAnalyzeAtKey, now.Add(-consts.DBAnalyzeMaxAge).Format(time.RFC3339Nano))
		putProperty(consts.DBAnalyzePendingKey, "0")

		ran, err := db.OptimizeDBIfNeeded(ctx, database, now)
		Expect(err).ToNot(HaveOccurred())
		Expect(ran).To(BeTrue())
		Expect(getProperty(consts.LastDBAnalyzeAtKey)).To(Equal(now.Format(time.RFC3339Nano)))
	})

	It("runs when a refresh is pending even if the previous analysis is recent", func() {
		putProperty(consts.LastDBAnalyzeAtKey, now.Format(time.RFC3339Nano))
		putProperty(consts.DBAnalyzePendingKey, "1")

		ran, err := db.OptimizeDBIfNeeded(ctx, database, now.Add(time.Hour))
		Expect(err).ToNot(HaveOccurred())
		Expect(ran).To(BeTrue())
		Expect(getProperty(consts.DBAnalyzePendingKey)).To(Equal("0"))
	})

	DescribeTable("backs off after consecutive analysis failures",
		func(failures string, retryDelay time.Duration) {
			putProperty(consts.DBAnalyzePendingKey, "1")
			putProperty(consts.DBAnalyzeFailureCountKey, failures)
			putProperty(consts.LastDBAnalyzeAttemptAtKey, now.Format(time.RFC3339Nano))

			ran, err := db.OptimizeDBIfNeeded(ctx, database, now.Add(retryDelay-time.Nanosecond))
			Expect(err).ToNot(HaveOccurred())
			Expect(ran).To(BeFalse())

			ran, err = db.OptimizeDBIfNeeded(ctx, database, now.Add(retryDelay))
			Expect(err).ToNot(HaveOccurred())
			Expect(ran).To(BeTrue())
			Expect(getProperty(consts.DBAnalyzeFailureCountKey)).To(Equal("0"))
			Expect(getProperty(consts.DBAnalyzePendingKey)).To(Equal("0"))
		},
		Entry("for 30 minutes after the first failure", "1", 30*time.Minute),
		Entry("for one hour after the second failure", "2", time.Hour),
		Entry("for two hours after the third failure", "3", 2*time.Hour),
		Entry("for 24 hours after the fourth failure", "4", 24*time.Hour),
	)

	It("records consecutive analysis failures", func() {
		putProperty(consts.DBAnalyzeFailureCountKey, "2")

		Expect(db.RecordAnalyzeFailure(ctx, database, now)).To(Succeed())

		Expect(getProperty(consts.DBAnalyzeFailureCountKey)).To(Equal("3"))
		Expect(getProperty(consts.LastDBAnalyzeAttemptAtKey)).To(Equal(now.Format(time.RFC3339Nano)))
		Expect(getProperty(consts.DBAnalyzePendingKey)).To(Equal("1"))
	})

	It("does not record success when analysis fails", func() {
		lastAnalyze := now.Add(-48 * time.Hour).Format(time.RFC3339Nano)
		putProperty(consts.LastDBAnalyzeAtKey, lastAnalyze)
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		Expect(db.OptimizeDBAt(canceledCtx, database, now)).To(MatchError(ContainSubstring("context canceled")))
		Expect(getProperty(consts.LastDBAnalyzeAtKey)).To(Equal(lastAnalyze))
	})
})
