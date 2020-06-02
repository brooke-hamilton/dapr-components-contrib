// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.
// ------------------------------------------------------------
package postgresql

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/dapr/components-contrib/state"
	"github.com/dapr/dapr/pkg/logger"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

const (
	connectionStringEnvKey = "DAPR_TEST_POSTGRES_CONNSTRING" // Environment variable containing the connection string
)

func TestPostgreSQLIntegration(t *testing.T) {
	connectionString := getConnectionString()
	if connectionString == "" {
		t.Skipf("PostgreSQL state integration tests skipped. To enable define the connection string using environment variable '%s' (example 'export %s=\"host=localhost user=postgres password=example port=5432 connect_timeout=10 database=dapr_test\")", connectionStringEnvKey, connectionStringEnvKey)
	}

	t.Run("Test init configurations", func(t *testing.T) {
		testInitConfiguration(t)
	})

	metadata := state.Metadata{
		Properties: map[string]string{connectionStringKey: connectionString},
	}

	pgs := NewPostgreSQLStateStore(logger.NewLogger("test"))
	t.Cleanup(func() {
		defer pgs.Close()
	})

	error := pgs.Init(metadata)
	if error != nil {
		t.Fatal(error)
	}

	// Can set and get an item.
	t.Run("Get Set Delete one item", func(t *testing.T) {
		t.Parallel()
		getSetUpdateDeleteOneItem(t, pgs)
	})

	// When an item does not exist, get should return a response with Data set to nil.
	t.Run("Get item that does not exist", func(t *testing.T) {
		t.Parallel()
		getItemThatDoesNotExist(t, pgs)
	})

	// Insert date and update date are correctly set and updated in the database
	t.Run("Set updates the updatedate field", func(t *testing.T) {
		t.Parallel()
		setUpdatesTheUpdatedateField(t, pgs)
	})

	// Bulk set and delete work
	t.Run("Bulk set and bulk delete", func(t *testing.T) {
		t.Parallel()
		testBulkSetAndBulkDelete(t, pgs)
	})

	// Updates and deletes with an etag are valid
	t.Run("Update and delete with etag succeeds", func(t *testing.T) {
		t.Parallel()
		updateAndDeleteWithEtagSucceeds(t, pgs)
	})

	// Updates with an etag are valid
	t.Run("Update with old etag fails", func(t *testing.T) {
		t.Parallel()
		updateWithOldEtagFails(t, pgs)
	})

	// Inserts should not have etags
	t.Run("Insert with etag fails", func(t *testing.T) {
		t.Parallel()
		newItemWithEtagFails(t, pgs)
	})

	// Delete with invalid etag fails
	t.Run("Delete with invalid etag fails", func(t *testing.T) {
		t.Parallel()
		deleteWithInvalidEtagFails(t, pgs)
	})
}

func deleteWithInvalidEtagFails(t *testing.T, pgs *PostgreSQL) {
	// Create new item
	key := uuid.New().String()
	value := `{"something": "ZPRw7DYBLgYA"}`
	setItem(t, pgs, key, value, "")

	// Delete the item with a fake etag
	deleteReq := &state.DeleteRequest{
		Key:  key,
		ETag: "1234",
	}
	err := pgs.Delete(deleteReq)
	assert.NotNil(t, err)
}

// newItemWithEtagFails creates a new item and also supplies an ETag, which is invalid - expect failure
func newItemWithEtagFails(t *testing.T, pgs *PostgreSQL) {
	value := `{"newthing2": "4xn7S2Dtberk"}`
	invalidEtag := "12345"

	setReq := &state.SetRequest{
		Key:   uuid.New().String(),
		ETag:  invalidEtag,
		Value: value,
	}

	err := pgs.Set(setReq)
	assert.NotNil(t, err)
}

func updateWithOldEtagFails(t *testing.T, pgs *PostgreSQL) {
	// Create and retrieve new item
	key := uuid.New().String()
	value := `{"something": "kCIcMsw8hTm7"}`
	setItem(t, pgs, key, value, "")
	getResponse := getItem(t, pgs, key)
	assert.NotNil(t, getResponse.ETag)
	originalEtag := getResponse.ETag

	// Change the value and get the updated etag
	newValue := `{"newthing": "Jh4K5bH9kogL"}`
	setItem(t, pgs, key, newValue, originalEtag)
	updatedItem := getItem(t, pgs, key)
	assert.Equal(t, newValue, string(updatedItem.Data))

	// Update again with the original etag - expect udpate failure
	newValue = `{"newthing2": "XEL2RuXSLZAu"}`
	setReq := &state.SetRequest{
		Key:   key,
		ETag:  originalEtag,
		Value: newValue,
	}
	err := pgs.Set(setReq)
	assert.NotNil(t, err)
}

func updateAndDeleteWithEtagSucceeds(t *testing.T, pgs *PostgreSQL) {
	// Create and retrieve new item
	key := uuid.New().String()
	value := `{"something": "QJucVIDPCU1r"}`
	setItem(t, pgs, key, value, "")
	getResponse := getItem(t, pgs, key)
	assert.NotNil(t, getResponse.ETag)

	// Change the value and compare
	newValue := `{"newthing": "eSg1q7RwbTuz"}`
	setItem(t, pgs, key, newValue, getResponse.ETag)
	updatedItem := getItem(t, pgs, key)
	assert.Equal(t, newValue, string(updatedItem.Data))

	// ETag should change when item is updated
	assert.NotEqual(t, getResponse.ETag, updatedItem.ETag)

	// Delete
	deleteItem(t, pgs, key, updatedItem.ETag)

	// Item is not in the data store
	assert.False(t, storeItemExists(t, key))
}

// getSetUpdateDeleteOneItem validates setting one item, getting it, and deleting it.
func getSetUpdateDeleteOneItem(t *testing.T, pgs *PostgreSQL) {
	key := uuid.New().String()
	value := `{"something": "DKbLaZwrlCAZ"}`

	setItem(t, pgs, key, value, "")

	getResponse := getItem(t, pgs, key)
	assert.Equal(t, value, string(getResponse.Data))

	newValue := `{"newthing": "bnfjSADNzspd"}`
	setItem(t, pgs, key, newValue, getResponse.ETag)
	newGetResponse := getItem(t, pgs, key)
	assert.Equal(t, newValue, string(newGetResponse.Data))

	deleteItem(t, pgs, key, "")
}

// getItemThatDoesNotExist validates the behavior of retrieving an item that does not exist
func getItemThatDoesNotExist(t *testing.T, pgs *PostgreSQL) {
	key := uuid.New().String()
	response := getItem(t, pgs, key)
	assert.Nil(t, response.Data)
}

// setUpdatesTheUpdatedateField proves that the updateddate is set for an update, and not set upon insert.
func setUpdatesTheUpdatedateField(t *testing.T, pgs *PostgreSQL) {
	key := uuid.New().String()
	value := `{"something": "FHKwv3jCX5Lc"}`
	setItem(t, pgs, key, value, "")

	// insertdate should have a value and updatedate should be nil
	retrievedValue, insertdate, updatedate := getRowData(t, key)
	assert.Equal(t, value, retrievedValue)
	assert.NotNil(t, insertdate)
	assert.Equal(t, "", updatedate.String)

	// insertdate should not change, updatedate should have a value
	value = `{"newthing": "bXuPvHltCXKL"}`
	setItem(t, pgs, key, value, "")
	retrievedValue, newinsertdate, updatedate := getRowData(t, key)
	assert.Equal(t, value, retrievedValue)
	assert.Equal(t, insertdate, newinsertdate) // The insertdate should not change.
	assert.NotEqual(t, "", updatedate.String)

	deleteItem(t, pgs, key, "")
}

// Tests valid bulk sets and deletes
func testBulkSetAndBulkDelete(t *testing.T, pgs *PostgreSQL) {
	setReq := []state.SetRequest{
		{
			Key:   uuid.New().String(),
			Value: `{"request1": "mSWIMIVlF5Gc"}`,
		},
		{
			Key:   uuid.New().String(),
			Value: `{"request2": "hc4Ay4ot8Gqm"}`,
		},
	}

	err := pgs.BulkSet(setReq)
	assert.Nil(t, err)
	assert.True(t, storeItemExists(t, setReq[0].Key))
	assert.True(t, storeItemExists(t, setReq[1].Key))

	deleteReq := []state.DeleteRequest{
		{
			Key: setReq[0].Key,
		},
		{
			Key: setReq[1].Key,
		},
	}

	err = pgs.BulkDelete(deleteReq)
	assert.Nil(t, err)
	assert.False(t, storeItemExists(t, setReq[0].Key))
	assert.False(t, storeItemExists(t, setReq[1].Key))
}

// testInitConfiguration tests valid and invalid config settings
func testInitConfiguration(t *testing.T) {
	logger := logger.NewLogger("test")
	tests := []struct {
		name        string
		props       map[string]string
		expectedErr string
	}{
		{
			name:        "Empty",
			props:       map[string]string{},
			expectedErr: errMissingConnectionString,
		},
		{
			name:        "Valid connection string",
			props:       map[string]string{connectionStringKey: getConnectionString()},
			expectedErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPostgreSQLStateStore(logger)
			defer p.Close()

			metadata := state.Metadata{
				Properties: tt.props,
			}

			err := p.Init(metadata)
			if tt.expectedErr == "" {
				assert.Nil(t, err)
			} else {
				assert.NotNil(t, err)
				assert.Equal(t, err.Error(), tt.expectedErr)
			}
		})
	}
}

func getConnectionString() string {
	return os.Getenv(connectionStringEnvKey)
}

func setItem(t *testing.T, pgs *PostgreSQL, key string, value string, etag string) {
	setReq := &state.SetRequest{
		Key:   key,
		ETag:  etag,
		Value: value,
	}

	err := pgs.Set(setReq)
	assert.Nil(t, err)
	assert.True(t, storeItemExists(t, key))
}

func getItem(t *testing.T, pgs *PostgreSQL, key string) *state.GetResponse {
	getReq := &state.GetRequest{
		Key:     key,
		Options: state.GetStateOption{},
	}

	response, getErr := pgs.Get(getReq)
	assert.Nil(t, getErr)
	assert.NotNil(t, response)
	return response
}

func deleteItem(t *testing.T, pgs *PostgreSQL, key string, etag string) {
	deleteReq := &state.DeleteRequest{
		Key:     key,
		ETag:    etag,
		Options: state.DeleteStateOption{},
	}

	deleteErr := pgs.Delete(deleteReq)
	assert.Nil(t, deleteErr)
	assert.False(t, storeItemExists(t, key))
}

func storeItemExists(t *testing.T, key string) bool {
	db, err := sql.Open("pgx", getConnectionString())
	assert.Nil(t, err)
	defer db.Close()

	var exists bool = false
	statement := fmt.Sprintf(`SELECT EXISTS (SELECT FROM %s WHERE key = $1)`, tableName)
	err = db.QueryRow(statement, key).Scan(&exists)
	assert.Nil(t, err)
	return exists
}

func getRowData(t *testing.T, key string) (returnValue string, insertdate sql.NullString, updatedate sql.NullString) {
	db, err := sql.Open("pgx", getConnectionString())
	assert.Nil(t, err)
	defer db.Close()

	err = db.QueryRow(fmt.Sprintf("SELECT value, insertdate, updatedate FROM %s WHERE key = $1", tableName), key).Scan(&returnValue, &insertdate, &updatedate)
	assert.Nil(t, err)
	return returnValue, insertdate, updatedate
}
