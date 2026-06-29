package migrations

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Create compute_instance_groups table", func() {
	It("Creates the compute_instance_groups table", func(ctx context.Context) {
		err := tool.Migrate(ctx, 70)
		Expect(err).ToNot(HaveOccurred())

		var exists bool
		row := conn.QueryRow(
			ctx,
			`select exists (
				select from information_schema.tables
				where table_name = 'compute_instance_groups'
			)`,
		)
		err = row.Scan(&exists)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})
})
