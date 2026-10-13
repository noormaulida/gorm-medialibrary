package medialibrary

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// Media mirrors Spatie's media table.
type Media struct {
	ID                   uint64         `gorm:"primaryKey;autoIncrement"`
	ModelType            string         `gorm:"size:255;not null;index:media_model_type_model_id_index;column:model_type"`
	ModelID              uint64         `gorm:"not null;index:media_model_type_model_id_index;column:model_id"`
	UUID                 *string        `gorm:"type:char(36);uniqueIndex"`
	CollectionName       string         `gorm:"size:255;not null;column:collection_name"`
	Name                 string         `gorm:"size:255;not null"`
	FileName             string         `gorm:"size:255;not null;column:file_name"`
	MimeType             *string        `gorm:"size:255;column:mime_type"`
	Disk                 string         `gorm:"size:255;not null"`
	ConversionsDisk      *string        `gorm:"size:255;column:conversions_disk"`
	Size                 uint64         `gorm:"not null"`
	Manipulations        datatypes.JSON `gorm:"type:json;not null"`
	CustomProperties     datatypes.JSON `gorm:"type:json;not null;column:custom_properties"`
	GeneratedConversions datatypes.JSON `gorm:"type:json;not null;column:generated_conversions"`
	ResponsiveImages     datatypes.JSON `gorm:"type:json;not null;column:responsive_images"`
	OrderColumn          *int           `gorm:"column:order_column"`
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
}

// TableName keeps Spatie's default table name.
func (Media) TableName() string { return "media" }

func emptyJSONObject() datatypes.JSON {
	return datatypes.JSON([]byte("{}"))
}

func emptyJSONArray() datatypes.JSON {
	return datatypes.JSON([]byte("[]"))
}

// GeneratedConversionMap decodes generated_conversions JSON.
func (m Media) GeneratedConversionMap() map[string]bool {
	out := map[string]bool{}
	if len(m.GeneratedConversions) == 0 {
		return out
	}
	_ = json.Unmarshal(m.GeneratedConversions, &out)
	return out
}

func encodeGeneratedConversions(m map[string]bool) datatypes.JSON {
	if m == nil {
		return emptyJSONObject()
	}
	b, err := jsonMarshal(m)
	if err != nil {
		return emptyJSONObject()
	}
	return datatypes.JSON(b)
}

// CustomPropertyMap decodes custom_properties JSON.
func (m Media) CustomPropertyMap() map[string]any {
	out := map[string]any{}
	if len(m.CustomProperties) == 0 {
		return out
	}
	_ = json.Unmarshal(m.CustomProperties, &out)
	return out
}

func encodeCustomProperties(m map[string]any) datatypes.JSON {
	if m == nil {
		return emptyJSONObject()
	}
	b, err := jsonMarshal(m)
	if err != nil {
		return emptyJSONObject()
	}
	return datatypes.JSON(b)
}

// ResponsiveImagesMap decodes responsive_images JSON (Spatie object shape).
func (m Media) ResponsiveImagesMap() ResponsiveImagesData {
	out := ResponsiveImagesData{}
	if len(m.ResponsiveImages) == 0 || string(m.ResponsiveImages) == "[]" || string(m.ResponsiveImages) == "null" {
		return out
	}
	_ = json.Unmarshal(m.ResponsiveImages, &out)
	return out
}

func encodeResponsiveImages(m ResponsiveImagesData) datatypes.JSON {
	if len(m) == 0 {
		return emptyJSONArray()
	}
	b, err := jsonMarshal(m)
	if err != nil {
		return emptyJSONArray()
	}
	return datatypes.JSON(b)
}
