package migrations

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Migrate templates to classes", func() {
	It("Runs without error", func(ctx context.Context) {
		err := tool.Migrate(ctx, 71)
		Expect(err).ToNot(HaveOccurred())
	})
})
