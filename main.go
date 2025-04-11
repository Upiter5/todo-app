package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Task struct {
	ID          int       `json:"id" validate:"-"`
	Title       string    `json:"title" validate:"required,min=3,max=100"`
	Description string    `json:"description" validate:"max=500"`
	Status      string    `json:"status" validate:"oneof=todo in_progress done"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var (
	db       *pgxpool.Pool
	validate = validator.New()
)

func main() {
	// Инициализация логгера
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Подключение к PostgreSQL
	dsn := "postgres://postgres:050298@localhost:5432/tododb?pool_max_conns=20"
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse DSN")
	}

	config.MaxConns = 20
	config.MinConns = 5
	config.HealthCheckPeriod = 1 * time.Minute
	config.MaxConnLifetime = 2 * time.Hour

	db, err = pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	// Инициализация Fiber
	app := fiber.New(fiber.Config{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	})

	// Роуты
	app.Post("/tasks", createTask)
	app.Get("/tasks", getTasks)
	app.Get("/tasks/:id", getTaskByID)
	app.Put("/tasks/:id", updateTask)
	app.Delete("/tasks/:id", deleteTask)

	// Graceful Shutdown
	go func() {
		if err := app.Listen(":8080"); err != nil {
			log.Fatal().Err(err).Msg("Server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")
	if err := app.Shutdown(); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}
	log.Info().Msg("Server stopped")
}

func createTask(c *fiber.Ctx) error {
	var task Task
	if err := c.BodyParser(&task); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	if err := validate.Struct(task); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	query := `INSERT INTO tasks (title, description, status) VALUES ($1, $2, $3) 
	          RETURNING id, created_at, updated_at`
	err := db.QueryRow(context.Background(), query, task.Title, task.Description, task.Status).
		Scan(&task.ID, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create task")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to create task")
	}

	return c.Status(fiber.StatusCreated).JSON(task)
}

func getTasks(c *fiber.Ctx) error {
	rows, err := db.Query(context.Background(),
		"SELECT id, title, description, status, created_at, updated_at FROM tasks")
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch tasks")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to fetch tasks")
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			log.Error().Err(err).Msg("Failed to scan task")
			continue
		}
		tasks = append(tasks, t)
	}

	return c.JSON(tasks)
}

func getTaskByID(c *fiber.Ctx) error {
	id := c.Params("id")
	var task Task

	query := `SELECT id, title, description, status, created_at, updated_at FROM tasks WHERE id = $1`
	err := db.QueryRow(context.Background(), query, id).Scan(
		&task.ID, &task.Title, &task.Description, &task.Status, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch task")
		return fiber.NewError(fiber.StatusNotFound, "Task not found")
	}

	return c.JSON(task)
}

func updateTask(c *fiber.Ctx) error {
	id := c.Params("id")
	var task Task

	if err := c.BodyParser(&task); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}

	if err := validate.Struct(task); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	query := `UPDATE tasks SET title=$1, description=$2, status=$3, updated_at=now() 
	          WHERE id=$4 RETURNING updated_at`
	err := db.QueryRow(context.Background(), query,
		task.Title, task.Description, task.Status, id).Scan(&task.UpdatedAt)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update task")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to update task")
	}

	return c.JSON(task)
}

func deleteTask(c *fiber.Ctx) error {
	id := c.Params("id")

	_, err := db.Exec(context.Background(), "DELETE FROM tasks WHERE id=$1", id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to delete task")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete task")
	}

	return c.SendStatus(fiber.StatusNoContent)
}
