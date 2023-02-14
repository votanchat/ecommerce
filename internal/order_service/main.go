package main

import (
	"encoding/json"
	"log"
	"os"

	"chatvo/shared/models"

	"github.com/gofiber/fiber/v2"
	"github.com/streadway/amqp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var ch *amqp.Channel
var db *gorm.DB

func main() {
	// Kết nối đến Posgres
	dsn := os.Getenv("DATABASE_URL")
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Tạo bảng Order trong Posgres (nếu chưa tồn tại)
	err = db.AutoMigrate(&models.Order{})
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
	ch, err = conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Tạo exchange để gửi message
	err = ch.ExchangeDeclare("orders", "fanout", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare an exchange: %v", err)
	}

	// Tạo queue để nhận tin nhắn từ exchange
	q, err := ch.QueueDeclare("orders", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Liên kết queue với exchange
	err = ch.QueueBind(q.Name, "", "orders", false, nil)
	if err != nil {
		log.Fatalf("Failed to bind the queue to the exchange: %v", err)
	}

	// Tạo go routine để lắng nghe và xử lý tin nhắn từ queue

	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}
	go func() {
		for msg := range msgs {
			order := &models.Order{}
			err := json.Unmarshal(msg.Body, order)
			if err != nil {
				log.Printf("Failed to parse message: %v", err)
				continue
			}
			err = updateOrder(order)
			if err != nil {
				log.Printf("Failed to update order: %v", err)
				continue
			}
		}
	}()
	// Khởi tạo Fiber App
	app := fiber.New()

	// Định nghĩa các route cho service đặt hàng
	app.Post("/orders", createOrder)

	// Khởi động server
	err = app.Listen(":3000")
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// Xử lý tạo đơn hàng mới
func createOrder(c *fiber.Ctx) error {
	order := new(models.Order)
	err := c.BodyParser(order)
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.JSON(order)
}

func updateOrder(order *models.Order) error {
	// Tìm đối tượng Order cũ trong cơ sở dữ liệu
	var oldOrder models.Order
	err := db.Where("id = ?", order.ID).First(&oldOrder).Error
	if err != nil {
		return err
	}

	// Nếu trạng thái đơn hàng không thay đổi, thì không cần update
	if oldOrder.Status == order.Status {
		return nil
	}

	// Cập nhật trạng thái đơn hàng
	err = db.Model(&oldOrder).Update("status", order.Status).Error
	if err != nil {
		return err
	}

	return nil
}
