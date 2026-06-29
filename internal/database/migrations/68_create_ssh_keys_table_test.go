package migrations

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Create ssh_keys table", func() {
	It("Creates the ssh_keys table", func(ctx context.Context) {
		err := tool.Migrate(ctx, 68)
		Expect(err).ToNot(HaveOccurred())

		var exists bool
		row := conn.QueryRow(
			ctx,
			`select exists (
				select from information_schema.tables
				where table_name = 'ssh_keys'
			)`,
		)
		err = row.Scan(&exists)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})
})
