/** Package runner is used to execute program task operations in sequence,
which can be used as cron job or timing task.
The runner package can be used to show how to use the channel to monitor
the execution time of the program.
If the program runs for too long, specify the task execution time.
This program may be executed as a cron job, or executed in a cloud environment based
on scheduled tasks (such as iron.io).

Supplementary note:
It may be used as a cron job or based on a timing task, which can control the execution time of the program
Use channels to monitor the execution time, life cycle, and even terminate the program.
Our program is called runner, we can call it the executor.
It can perform any task in the background, and we can also control the executor, such as forcibly terminating it, etc.
In addition, this executor is also a very good mode. For example, after we write it,
we can hand it over to the timing task to execute it.
For example, cron, in this mode we can also extend more efficient concurrency,
more flexible control of the life cycle of the program
More efficient monitoring, etc.
*/
package runner

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	// ErrTimeout task exec timeout
	ErrTimeout = errors.New("task exec timeout")

	// ErrInterrupt recv interrupt signal
	ErrInterrupt = errors.New("received interrupt signal")
)

// Logger log interface
type Logger interface {
	Println(msg ...interface{})
}

// Runner 声明一个runner
type Runner struct {
	complete   chan error       // 有缓冲通道，存放所有任务运行后的结果状态
	tasks      []func() error   // 执行的任务func,如果func没有错误返回，可以返回nil
	timeout    time.Duration    // 所有的任务超时时间
	timeCh     <-chan time.Time // 任务超时通道
	logger     Logger           // 日志输出实例
	interrupt  chan os.Signal   // 可以控制强制终止的信号
	allErrors  map[int]error    // 发生错误的task index对应的错误
	lastTaskId int              // 最后一次完成的任务id
}

// Option 采用func Option功能模式为Runner添加参数
type Option func(r *Runner)

// New 定义一个工厂函数创建runner
// 默认创建一个无超时任务的runner
func New(opts ...Option) *Runner {
	r := &Runner{
		complete:  make(chan error, 1),
		interrupt: make(chan os.Signal, 1), // 声明一个中断信号
	}

	// 初始化option
	for _, o := range opts {
		o(r)
	}

	if r.logger == nil {
		r.logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	return r
}

// WithTimeout 设置任务超时时间
func WithTimeout(t time.Duration) Option {
	return func(r *Runner) {
		r.timeout = t
	}
}

// WithLogger 设置r.logger打印日志的句柄
func WithLogger(l Logger) Option {
	return func(r *Runner) {
		r.logger = l
	}
}

// Add 将需要执行的任务添加到r.tasks队列中
func (r *Runner) Add(tasks ...func() error) {
	r.tasks = append(r.tasks, tasks...)
}

// run 运行一个个任务,如果出错就返回错误信息
func (r *Runner) run() (err error) {
	for k, task := range r.tasks {
		r.lastTaskId = k

		if r.isInterrupt() {
			err = ErrInterrupt
			return
		}

		r.logger.Println("current run task id: ", k)

		err = r.doTask(task)
		if err != nil {
			r.logger.Println("current task exec occur error: ", err)
			r.allErrors[k] = err
		}
	}

	return
}

// doTask 执行每个task，需要捕获每个任务是否出现了panic异常
// 防止一些个别任务出现了panic,从而导致整个tasks执行全部退出
func (r *Runner) doTask(task func() error) (err error) {
	defer func() {
		if e := recover(); e != nil {
			r.logger.Println("current task throw panic: ", e)
			err = fmt.Errorf("current task panic: %v", e)
		}
	}()

	err = task()

	return
}

// GetAllErrors 获取已经完成任务的error
func (r *Runner) GetAllErrors() map[int]error {
	return r.allErrors
}

// GetLastTaskId 获取最后一次完成任务id
func (r *Runner) GetLastTaskId() int {
	return r.lastTaskId
}

// Start 开始执行所有的任务
func (r *Runner) Start() error {
	// 接收系统退出信号
	signal.Notify(r.interrupt, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, syscall.SIGHUP)

	r.allErrors = make(map[int]error, len(r.tasks)+1)

	if r.timeout > 0 {
		r.timeCh = time.After(r.timeout)
	}

	// 执行完毕的信号量
	done := make(chan struct{}, 1)

	// 开启独立goroutine执行任务
	go func() {
		defer func() {
			if e := recover(); e != nil {
				r.logger.Println("exec task panic: ", e)
			}

			close(done)
		}()

		r.complete <- r.run()
	}()

	select {
	case <-r.timeCh:
		r.logger.Println(ErrTimeout)
		return ErrTimeout
	case <-done:
		err := <-r.complete
		r.logger.Println("task complete status: ", err)
		return err
	}
}

// isInterrupt 检查是否接受到操作系统的中断信号
// 一旦r.interrupt中可以接收值，就会通知Go Runtime停止接收中断信号，然后返回true
// 这里如果没有default的话，select是会阻塞的，直到r.interrupt可以接收值为止
func (r *Runner) isInterrupt() bool {
	select {
	case sg := <-r.interrupt: // 是否接受到操作系统的中断信号
		signal.Stop(r.interrupt)
		r.logger.Println("received signal: ", sg.String())

		return true
	default:
		return false
	}
}
