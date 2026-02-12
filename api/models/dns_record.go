package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type DnsRecord struct {
	ID               string    `json:"id"`
	DomainID         string    `json:"domain_id"`
	RecordType       string    `json:"record_type"`
	Name             string    `json:"name"`
	Value            string    `json:"value"`
	TTL              int       `json:"ttl"`
	Priority         int       `json:"priority"`
	Managed          bool      `json:"managed"`
	ProviderRecordID string    `json:"provider_record_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func CreateDnsRecord(r *DnsRecord) error {
	return database.DB.QueryRow(
		`INSERT INTO dns_records (domain_id, record_type, name, value, ttl, priority, managed, provider_record_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at, updated_at`,
		r.DomainID, r.RecordType, r.Name, r.Value, r.TTL, r.Priority, r.Managed, r.ProviderRecordID,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

func GetDnsRecord(id string) (*DnsRecord, error) {
	r := &DnsRecord{}
	err := database.DB.QueryRow(
		`SELECT id, domain_id, record_type, name, value, COALESCE(ttl,3600), COALESCE(priority,0),
		        COALESCE(managed,false), COALESCE(provider_record_id,''), created_at, updated_at
		 FROM dns_records WHERE id=$1`, id,
	).Scan(&r.ID, &r.DomainID, &r.RecordType, &r.Name, &r.Value, &r.TTL, &r.Priority,
		&r.Managed, &r.ProviderRecordID, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func ListDnsRecordsByDomain(domainID string) ([]DnsRecord, error) {
	rows, err := database.DB.Query(
		`SELECT id, domain_id, record_type, name, value, COALESCE(ttl,3600), COALESCE(priority,0),
		        COALESCE(managed,false), COALESCE(provider_record_id,''), created_at, updated_at
		 FROM dns_records WHERE domain_id=$1 ORDER BY record_type, name`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []DnsRecord
	for rows.Next() {
		var r DnsRecord
		if err := rows.Scan(&r.ID, &r.DomainID, &r.RecordType, &r.Name, &r.Value, &r.TTL, &r.Priority,
			&r.Managed, &r.ProviderRecordID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func UpdateDnsRecord(r *DnsRecord) error {
	_, err := database.DB.Exec(
		`UPDATE dns_records SET record_type=$1, name=$2, value=$3, ttl=$4, priority=$5, updated_at=NOW() WHERE id=$6`,
		r.RecordType, r.Name, r.Value, r.TTL, r.Priority, r.ID)
	return err
}

func DeleteDnsRecord(id string) error {
	_, err := database.DB.Exec(`DELETE FROM dns_records WHERE id=$1`, id)
	return err
}
