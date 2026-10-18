package fmsodata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSchemaModificationIntegration(t *testing.T) {
	// Use credentials provided by the user
	config := ClientConfig{
		Host:     "https://your-host",
		Database: "your-database",
		Username: "your-username",
		Password: "your-password",
		Timeout:  30 * time.Second,
	}

	client := NewClient(config)
	ctx := context.Background()

	tableName := "TestTable_Integration"

	// Clean up before test just in case
	_ = client.DeleteTable(ctx, tableName)

	// 1. Create Table
	t.Run("CreateTable", func(t *testing.T) {
		tableDef := TableDefinition{
			TableName: tableName,
			Fields: []FieldDefinition{
				{Name: "ID", Type: "NUMERIC", Primary: true, Unique: true},
				{Name: "Name", Type: "VARCHAR"},
				{Name: "Description", Type: "VARCHAR"},
			},
		}
		err := client.CreateTable(ctx, tableDef)
		assert.NoError(t, err, "Failed to create table")
	})

	// 2. Create Index
	t.Run("CreateIndex", func(t *testing.T) {
		err := client.CreateIndex(ctx, tableName, "Name")
		assert.NoError(t, err, "Failed to create index")
	})

	// 3. Verify Table Exists (by trying to create a record)
	t.Run("VerifyTableExists", func(t *testing.T) {
		record := map[string]interface{}{
			"ID":          1,
			"Name":        "Test Item",
			"Description": "This is a test item",
		}
		_, err := client.CreateRecord(ctx, tableName, record)
		assert.NoError(t, err, "Failed to create record in new table")
	})

	// 4. Delete Index
	t.Run("DeleteIndex", func(t *testing.T) {
		err := client.DeleteIndex(ctx, tableName, "Name")
		assert.NoError(t, err, "Failed to delete index")
	})

	// 5. Delete Table
	t.Run("DeleteTable", func(t *testing.T) {
		err := client.DeleteTable(ctx, tableName)
		assert.NoError(t, err, "Failed to delete table")
	})
}
