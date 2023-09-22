package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// В программе есть несколько сервисов. При запуске программа стартует все сервисы последовательно,
// вызывая метод Start(). Потом ждет сигнала от ОС на завершение (SIGINT, SIGTERM). После получения
// сигнала, останавливает сервисы в обратном порядке, вызывая метод Stop(). Если сервисы стартовались
// в порядке A-B-C, то останавливаться должны в порядке C-B-A. Если на всех этапах проблем не было,
// код возврата программа должен быть 0, в случае проблемы код возврата должен быть 1 (`os.Exit(1)`).
//
// Таким образом жизненный цикл программы состоит из трех этапов: инициализация, работа, завершение.
// Если в процессе жизненного цикла программы была проблема, код возврата должен быть 1.
// Все проблемы логируются.
//
// Еще условия:
//
// * Методы Start и Stop принимают контекст. Дать им контекст в 5 секунд.
// * Если Start одного из сервисов вернул ошибку, то требуется прекратить инициализацию,
//   остановить все уже запущенные сервисы (в обратном порядке) и остановить программу.
// * Если Stop вернул ошибку, логируем ее и продолжаем останавливать оставшиеся сервисы.
//
// ----------------
//
// Можно реализовать всю логику в main(), также можно создавать вспомогательные структуры.
// Писать тесты не нужно, создавать тестовые сервисы не обязательно. Нужно притвориться,
// будто список с сервисами существует.

type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type MockService struct {
	name string
	//Можно задать время старта сервиса самому
	longStartTime int
	//Статус, был ли запущен сервис
	status bool
}

func New(name string, longStartTime int) Service {
	return &MockService{
		name:          name,
		longStartTime: longStartTime,
		status:        false,
	}
}

func (ms *MockService) Start(ctx context.Context) error {
	doneStarting := make(chan struct{})
	// представим, что самый худший случай запуска севриса - вечноть,
	// поэтому обернем запуск в горутину, и будем ждать конца через канал
	go func() {
		log.Printf("[INFO] starting service %s...", ms.name)
		time.Sleep(time.Second * time.Duration(ms.longStartTime))
		ms.status = true
		doneStarting <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("time limit exceeded while starting service %s", ms.name)
	case <-doneStarting:
		return nil
	}
}

func (ms *MockService) Stop(ctx context.Context) error {
	doneStarting := make(chan struct{})
	// Делаем также, как и для старта сервисов
	if !ms.status {
		return nil
	}
	go func() {
		log.Printf("[INFO] stopping service %s...", ms.name)
		time.Sleep(time.Second * 1)
		doneStarting <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("time limit exceeded while stopping service %s", ms.name)
	case <-doneStarting:
		return nil
	}
}

func generateSIGTERM() error {
	pid := os.Getpid()
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("can't find a process: %s", err.Error())
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		return fmt.Errorf("can't send SIGTERM to a process: %s", err.Error())
	}

	return nil
}

func main() {
	// Предположим, что существует несколько сервисов
	var services []Service
	services = append(services, New("A", 1))
	services = append(services, New("B", 1))
	services = append(services, New("C", 3))

	sysExit := make(chan os.Signal, 1)
	signal.Notify(sysExit, syscall.SIGINT, syscall.SIGTERM)
	//Переменная, чтобы отличить, хорошо программа закрылась, или нет
	globalProgramStatus := 0

	// 1. Стартуем сервисы
	for _, service := range services {
		// Даем каждому сервису по 5 секунд на запуск, хотя зависит от наших целей,
		// нужно дать в целом на запуск программы 5 секунд, либо каждому сервису
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
		defer cancel()
		if err := service.Start(ctx); err != nil {
			log.Printf("[ERROR] can't start service: %s", err.Error())
			globalProgramStatus = 1
			_ = generateSIGTERM()
			break
		}
	}

	// 2. Ждем сигнала от ОС (если старт прошел успешно)
	<-sysExit

	// 3. Останавливаем сервисы в обратном порядке
	log.Println("[INFO] Shutting down...")

	for index := range services {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		//отсчет в обратном порядке
		serviceIndex := (len(services) - 1) - index
		if err := services[serviceIndex].Stop(ctx); err != nil {
			log.Printf("[ERROR] can't stop service: %s", err.Error())
			globalProgramStatus = 1
		}
	}

	fmt.Println("Working with status:", globalProgramStatus)
	os.Exit(globalProgramStatus)
}
