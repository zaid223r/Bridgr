# Bridgr

A modular, extensible Go library for building RESTful APIs with:
- Generic CRUD operations
- Per-model and generic filtering (with partial match for strings)
- Auto-generated OpenAPI/Swagger documentation
- Custom error handling
- Pluggable database support (GORM/Postgres)

---

## Features
- **Generic CRUD**: Register CRUD endpoints for any model with a single call.
- **Filtering**: 
  - Per-model: Specify filterable fields with `FilterableFields()`
  - Generic: Opt-in to all fields with `GenericFiltering()`
  - String fields use partial match (LIKE), others use exact match
- **OpenAPI/Swagger**: 
  - Auto-generates docs with correct filter query parameters and types
  - Fails fast if `FilterableFields` contains invalid fields
- **Custom Errors**: 
  - Invalid filter fields return clear 400 errors
  - All error types are centralized in the `errors/` package

---

## Quick Start

### 1. Define Your Models
```go
package models

type Todo struct {
    ID    uint   `json:"id" gorm:"primaryKey"`
    Title string `json:"title"`
    Done  bool   `json:"done"`
}
// All fields filterable
func (Todo) GenericFiltering() bool { return true }

type Note struct {
    ID    uint   `json:"id" gorm:"primaryKey"`
    Title string `json:"title"`
    Body  string `json:"body"`
}
// Only title is filterable
func (Note) FilterableFields() []string { return []string{"title"} }
```

### 2. Register CRUD and Start Server
```go
import (
    brcrud "bridgr/core/brcrud"
    brdb "bridgr/core/brdb"
    brhttp "bridgr/core/brhttp"
    "your/module/models"
)

db, err := brdb.ConnectPostgres(...)
if err != nil { panic(err) }
_ = db.AutoMigrate(&models.Todo{}, &models.Note{})

router := brhttp.NewRouter()
brcrud.RegisterCRUD[models.Todo](router, "todos", db, nil)
brcrud.RegisterCRUD[models.Note](router, "notes", db, nil)
brhttp.StartServer(router, "8080")
```

### 3. Filtering
- `/todos?title=foo&done=true` (all fields filterable)
- `/notes?title=bar` (only title is filterable)
- String fields use partial match (LIKE), others use exact match

### 4. OpenAPI/Swagger
- Visit `/docs` for Swagger UI
- Visit `/openapi.json` for the OpenAPI spec
- Filterable fields are shown as query parameters with correct types
- If `FilterableFields` contains an invalid field, the server will panic at startup

### 5. Error Handling
- Invalid filter fields return a 400 with a clear error message
- All custom errors are in the `errors/` package for easy expansion

---

## Extending
- Add new error types to `errors/`
- Add new models and register with CRUD as shown above
- Customize filtering logic or OpenAPI generation as needed

---

## License
MIT 