package models

import (
	"time"

	"github.com/railpush/api/database"
)

type ServiceInstance struct {
	ID          string    `json:"id"`
	ServiceID   string    `json:"service_id"`
	ContainerID string    `json:"container_id"`
	HostPort    int       `json:"host_port"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func CreateServiceInstance(si *ServiceInstance) error {
	return database.DB.QueryRow(
		`INSERT INTO service_instances (service_id, container_id, host_port, role, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,NOW(),NOW())
		 RETURNING id, created_at, updated_at`,
		si.ServiceID, si.ContainerID, si.HostPort, si.Role, si.Status,
	).Scan(&si.ID, &si.CreatedAt, &si.UpdatedAt)
}

func ListServiceInstances(serviceID string) ([]ServiceInstance, error) {
	rows, err := database.DB.Query(
		`SELECT id, service_id, container_id, host_port, COALESCE(role,'replica'), COALESCE(status,'live'), created_at, updated_at
		   FROM service_instances
		  WHERE service_id=$1
		  ORDER BY created_at ASC`,
		serviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceInstance
	for rows.Next() {
		var si ServiceInstance
		if err := rows.Scan(&si.ID, &si.ServiceID, &si.ContainerID, &si.HostPort, &si.Role, &si.Status, &si.CreatedAt, &si.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, nil
}

func DeleteServiceInstance(id string) error {
	_, err := database.DB.Exec("DELETE FROM service_instances WHERE id=$1", id)
	return err
}

func DeleteServiceInstancesByService(serviceID string) error {
	_, err := database.DB.Exec("DELETE FROM service_instances WHERE service_id=$1", serviceID)
	return err
}
