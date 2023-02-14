package models

import "gorm.io/gorm"

// Cấu trúc đơn hàng
type Order struct {
	gorm.Model
	CustomerName string `json:"customer_name"`
	ProductName  string `json:"product_name"`
	Quantity     int    `json:"quantity"`
	Status       string `json:"status"`
}
