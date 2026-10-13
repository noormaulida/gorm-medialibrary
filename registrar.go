package medialibrary

// CollectionConfig describes a named media collection on a model type.
type CollectionConfig struct {
	Name             string
	SingleFile       bool
	AcceptsMimeTypes []string
	ResponsiveImages bool
	Disk             string // optional; empty → library default disk
	ConversionsDisk  string // optional; empty → same as Disk
}

// ConversionConfig describes an image conversion.
type ConversionConfig struct {
	Name             string
	Collection       string
	Width            int
	Height           int
	Crop             bool
	Quality          int
	ResponsiveImages bool
}

// Registrar collects collection and conversion definitions for a model type.
type Registrar struct {
	collections map[string]*CollectionConfig
	conversions map[string]*ConversionConfig
}

func newRegistrar() *Registrar {
	return &Registrar{
		collections: map[string]*CollectionConfig{},
		conversions: map[string]*ConversionConfig{},
	}
}

// Collection starts (or returns) a collection definition.
func (r *Registrar) Collection(name string) *CollectionBuilder {
	cfg, ok := r.collections[name]
	if !ok {
		cfg = &CollectionConfig{Name: name}
		r.collections[name] = cfg
	}
	return &CollectionBuilder{reg: r, cfg: cfg}
}

// Conversion starts a conversion definition.
func (r *Registrar) Conversion(name string) *ConversionBuilder {
	cfg, ok := r.conversions[name]
	if !ok {
		cfg = &ConversionConfig{Name: name, Quality: 90}
		r.conversions[name] = cfg
	}
	return &ConversionBuilder{cfg: cfg}
}

// CollectionBuilder configures a collection.
type CollectionBuilder struct {
	reg *Registrar
	cfg *CollectionConfig
}

// SingleFile keeps only the latest media item in the collection.
func (b *CollectionBuilder) SingleFile() *CollectionBuilder {
	b.cfg.SingleFile = true
	return b
}

// AcceptsMimeTypes whitelists MIME types for uploads into this collection.
func (b *CollectionBuilder) AcceptsMimeTypes(mimes ...string) *CollectionBuilder {
	b.cfg.AcceptsMimeTypes = append([]string{}, mimes...)
	return b
}

// WithResponsiveImages generates width variants for the original file in this collection.
func (b *CollectionBuilder) WithResponsiveImages() *CollectionBuilder {
	b.cfg.ResponsiveImages = true
	return b
}

// Disk sets the storage disk for originals in this collection.
func (b *CollectionBuilder) Disk(name string) *CollectionBuilder {
	b.cfg.Disk = name
	return b
}

// ConversionsDisk sets the disk for conversions and responsive images.
func (b *CollectionBuilder) ConversionsDisk(name string) *CollectionBuilder {
	b.cfg.ConversionsDisk = name
	return b
}

// ConversionBuilder configures an image conversion.
type ConversionBuilder struct {
	cfg *ConversionConfig
}

// ForCollection scopes the conversion to media in collection.
func (b *ConversionBuilder) ForCollection(name string) *ConversionBuilder {
	b.cfg.Collection = name
	return b
}

// Fit resizes to fit inside width x height preserving aspect ratio.
func (b *ConversionBuilder) Fit(width, height int) *ConversionBuilder {
	b.cfg.Width = width
	b.cfg.Height = height
	b.cfg.Crop = false
	return b
}

// Crop resizes and center-crops to exact width x height.
func (b *ConversionBuilder) Crop(width, height int) *ConversionBuilder {
	b.cfg.Width = width
	b.cfg.Height = height
	b.cfg.Crop = true
	return b
}

// Quality sets JPEG encode quality (1-100).
func (b *ConversionBuilder) Quality(q int) *ConversionBuilder {
	if q > 0 && q <= 100 {
		b.cfg.Quality = q
	}
	return b
}

// WithResponsiveImages generates width variants for this conversion.
func (b *ConversionBuilder) WithResponsiveImages() *ConversionBuilder {
	b.cfg.ResponsiveImages = true
	return b
}
