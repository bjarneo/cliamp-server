package geo

import (
	"net"

	"github.com/oschwald/geoip2-golang"
)

// Location holds geo information for a client IP.
type Location struct {
	Country     string
	CountryCode string
	City        string
	Latitude    float64
	Longitude   float64
}

// DB wraps a MaxMind GeoLite2-City database reader.
type DB struct {
	reader *geoip2.Reader
}

// Open opens a MaxMind .mmdb file at the given path.
func Open(path string) (*DB, error) {
	reader, err := geoip2.Open(path)
	if err != nil {
		return nil, err
	}
	return &DB{reader: reader}, nil
}

// Lookup returns the geo location for the given IP string.
// Returns a zero Location on any error (invalid IP, not found, private range, etc.).
func (db *DB) Lookup(ipStr string) Location {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return Location{}
	}

	record, err := db.reader.City(ip)
	if err != nil {
		return Location{}
	}

	return Location{
		Country:     record.Country.Names["en"],
		CountryCode: record.Country.IsoCode,
		City:        record.City.Names["en"],
		Latitude:    record.Location.Latitude,
		Longitude:   record.Location.Longitude,
	}
}

// Close closes the underlying database reader.
func (db *DB) Close() error {
	return db.reader.Close()
}
