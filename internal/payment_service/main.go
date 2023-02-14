package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"chatvo/shared/models"

	"github.com/gofiber/fiber/v2"
	"github.com/streadway/amqp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB
var conn *amqp.Connection

func main() {
	// Kết nối đến Posgres
	dsn := os.Getenv("DATABASE_URL")
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Tạo bảng Payment trong Posgres (nếu chưa tồn tại)
	err = db.AutoMigrate(&Payment{})
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Kết nối đến RabbitMQ
	conn, err := amqp.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Tạo channel để gửi message
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Tạo exchange và queue để nhận message
	err = ch.ExchangeDeclare("payments", "fanout", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare an exchange: %v", err)
	}
	q, err := ch.QueueDeclare("", false, false, true, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}
	err = ch.QueueBind(q.Name, "", "payments", false, nil)
	if err != nil {
		log.Fatalf("Failed to bind the queue to the exchange: %v", err)
	}

	// Consume message
	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	// Xử lý message
	go func() {
		for d := range msgs {
			payment := new(Payment)
			err := json.Unmarshal(d.Body, payment)
			if err != nil {
				log.Printf("Failed to unmarshal message: %v", err)
				continue
			}
			payment.Status = "paid"
			err = db.Save(payment).Error
			if err != nil {
				log.Printf("Failed to save payment: %v", err)
			}
			log.Printf("Received a payment message: %v", payment)
		}
	}()

	// Khởi tạo Fiber App
	app := fiber.New()

	// Định nghĩa các route cho service thanh toán
	app.Post("/payments", createPayment)
	app.Get("/payments", getAllPayments)

	// Khởi động server
	err = app.Listen(":3001")
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// Cấu trúc thanh toán
type Payment struct {
	gorm.Model
	OrderID uint   `json:"order_id"`
	Amount  int    `json:"amount"`
	Status  string `json:"status"`
}

// Xử lý tạo thanh toán mới
func createPayment(c *fiber.Ctx) error {
	payment := new(Payment)
	err := c.BodyParser(payment)
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	var order models.Order
	err = db.First(&order, payment.OrderID).Error
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if order.Status != "pending" {
		return c.Status(400).SendString("Order status is not pending")
	}
	ch, err := conn.Channel()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer ch.Close()
	err = ch.Publish("payments", "", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(fmt.Sprintf(`{"order_id":%d,"amount":%d,"status":"pending"}`, payment.OrderID, payment.Amount)),
	})

	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	err = ch.Publish("orders", "", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(fmt.Sprintf(`{"id":%d,"status":"paid"}`, payment.OrderID)),
	})
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.JSON(payment)
}

// Xử lý lấy danh sách thanh toán
func getAllPayments(c *fiber.Ctx) error {
	var payments []Payment
	result := db.Find(&payments)
	if result.Error != nil {
		return c.Status(500).SendString(result.Error.Error())
	}
	return c.JSON(payments)
}
