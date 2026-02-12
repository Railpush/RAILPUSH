package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type Disk struct {
	ID        string    `json:"id"`
	ServiceID string    `json:"service_id"`
	Name      string    `json:"name"`
	MountPath string    `json:"mount_path"`
	SizeGB    int       `json:"size_gb"`
	CreatedAt time.Time `json:"created_at"`
}

func CreateDisk(d *Disk) error {
	return database.DB.QueryRow("INSERT INTO disks (service_id, name, mount_path, size_gb) VALUES ($1,$2,$3,$4) RETURNING id, created_at",
		d.ServiceID, d.Name, d.MountPath, d.SizeGB).Scan(&d.ID, &d.CreatedAt)
}

func GetDiskByService(serviceID string) (*Disk, error) {
	d := &Disk{}
	err := database.DB.QueryRow("SELECT id, service_id, name, mount_path, size_gb, created_at FROM disks WHERE service_id=$1", serviceID).Scan(
		&d.ID, &d.ServiceID, &d.Name, &d.MountPath, &d.SizeGB, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func DeleteDisk(id string) error {
	_, err := database.DB.Exec("DELETE FROM disks WHERE id=$1", id)
	return err
}
