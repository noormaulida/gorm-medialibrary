package medialibrary

import (
	"fmt"
	"reflect"
)

// HasMedia marks a model that can own media rows.
//
// ModelType should be a stable morph alias (for example "seo_metas") when
// sharing a database with Laravel.
type HasMedia interface {
	MediaModelID() uint64
	MediaModelType() string
}

// ModelFrom builds a HasMedia value from an explicit id and morph type.
func ModelFrom(id uint64, modelType string) HasMedia {
	return staticMediaModel{id: id, modelType: modelType}
}

type staticMediaModel struct {
	id        uint64
	modelType string
}

func (m staticMediaModel) MediaModelID() uint64   { return m.id }
func (m staticMediaModel) MediaModelType() string { return m.modelType }

func resolveModel(model HasMedia) (id uint64, typ string, err error) {
	if model == nil {
		return 0, "", fmt.Errorf("%w: model is nil", ErrInvalidArgument)
	}
	id = model.MediaModelID()
	if id == 0 {
		return 0, "", fmt.Errorf("%w: model id cannot be zero", ErrInvalidArgument)
	}
	typ = model.MediaModelType()
	if typ == "" {
		return 0, "", fmt.Errorf("%w: model type cannot be empty", ErrInvalidArgument)
	}
	return id, typ, nil
}

// PrimaryKeyID extracts a numeric primary key from a struct value.
func PrimaryKeyID(model any) (uint64, error) {
	if model == nil {
		return 0, fmt.Errorf("%w: model is nil", ErrInvalidArgument)
	}
	v := reflect.ValueOf(model)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, fmt.Errorf("%w: model is nil", ErrInvalidArgument)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, fmt.Errorf("%w: model must be a struct", ErrInvalidArgument)
	}
	field := v.FieldByName("ID")
	if !field.IsValid() {
		return 0, fmt.Errorf("%w: model has no ID field", ErrInvalidArgument)
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		id := field.Uint()
		if id == 0 {
			return 0, fmt.Errorf("%w: model id cannot be zero", ErrInvalidArgument)
		}
		return id, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		id := field.Int()
		if id <= 0 {
			return 0, fmt.Errorf("%w: model id must be positive", ErrInvalidArgument)
		}
		return uint64(id), nil
	default:
		return 0, fmt.Errorf("%w: unsupported ID type %s", ErrInvalidArgument, field.Kind())
	}
}
