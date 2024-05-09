package main

import (
	"image"
	"image/color"
	"log"
	"sync"
	"time"

	"github.com/jdavasligil/go-ecs"
	"golang.org/x/exp/shiny/driver"
	"golang.org/x/exp/shiny/screen"
	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/mouse"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
)

const (
	WIDTH    = 1280
	HEIGHT   = 720
	DELTA    = 1.0 / 60.0
	SIMTICK  = time.Second / 60
	DRAWTICK = time.Second / 30
	MAXSAND  = 460800
	GRAVITY  = float32(32.0) // px/s/s
)

var (
	blue  = color.RGBA{0x00, 0x00, 0x1f, 0xff}
	white = color.RGBA{0xee, 0xee, 0xee, 0xff}
	black = color.RGBA{0x05, 0x05, 0x05, 0xff}
)

func main() {
	driver.Main(func(s screen.Screen) {
		eventChan := make(chan any, 2)
		gridLocal := NewGrid()
		shared := Shared{}
		shared.grid = NewGrid()

		opts := &screen.NewWindowOptions{
			Width:  WIDTH,
			Height: HEIGHT,
			Title:  "Sandbox",
		}

		w, err := s.NewWindow(opts)
		if err != nil {
			log.Fatal(err)
		}
		defer w.Release()

		bsize := image.Point{WIDTH, HEIGHT}

		buf, err := s.NewBuffer(bsize)
		if err != nil {
			log.Fatal(err)
		}
		defer buf.Release()

		tex, err := s.NewTexture(bsize)
		if err != nil {
			log.Fatal(err)
		}
		defer tex.Release()
		tex.Fill(tex.Bounds(), black, screen.Src)

		go Simulate(&w, eventChan, &shared)

		var sz size.Event
		for {
			switch e := w.NextEvent().(type) {
			case lifecycle.Event:
				if e.To == lifecycle.StageDead {
					return
				}
			case key.Event:
				if e.Code == key.CodeEscape {
					return
				}
			case mouse.Event:
				select {
				case eventChan <- e:
				default:
				}
			case paint.Event:
				if e.External {
					continue
				}
				shared.mu.Lock()
				copy(gridLocal.data, shared.grid.data)
				shared.mu.Unlock()
				DrawGrid(&gridLocal, buf.RGBA())
				tex.Upload(image.Point{}, buf, buf.Bounds())
				w.Scale(sz.Bounds(), tex, tex.Bounds(), screen.Src, nil)
				w.Copy(image.Point{}, tex, tex.Bounds(), screen.Src, nil)
				w.Publish()
			case size.Event:
				sz = e
			case error:
				log.Print(e)
			default:
			}
		}
	})
}

func DrawGrid(g *Grid, img *image.RGBA) {
	for x := 0; x < WIDTH; x++ {
		for y := 0; y < HEIGHT; y++ {
			if g.IsSet(x, y) {
				img.SetRGBA(x, y, white)
			} else {
				img.SetRGBA(x, y, black)
			}
		}
	}
}

type Shared struct {
	mu   sync.Mutex
	grid Grid
}

// ECS TYPES
const (
	PositionID ecs.ComponentID = iota
	VelocityID
	FallingID
)

type Position struct {
	X, Y float32
}

func (p Position) ID() ecs.ComponentID {
	return PositionID
}

type Velocity struct {
	X, Y float32
}

func (v Velocity) ID() ecs.ComponentID {
	return VelocityID
}

type Falling struct{}

func (f Falling) ID() ecs.ComponentID {
	return FallingID
}

func InitializeWorld(world *ecs.World) {
	ecs.Initialize[Position](world)
	ecs.Initialize[Velocity](world)
	ecs.Initialize[Falling](world)
}

// OBJECTS
type Source struct {
	X, Y     float32
	isActive bool
}

type Grid struct {
	sync.Mutex
	data []bool
}

func NewGrid() Grid {
	return Grid{
		data: make([]bool, WIDTH*(HEIGHT+1)),
	}
}

func (g *Grid) IsSet(x, y int) bool {
	return g.data[x+WIDTH*y]
}

func (g *Grid) Set(x, y int) {
	g.data[x+WIDTH*y] = true
}

func (g *Grid) Clear(x, y int) {
	g.data[x+WIDTH*y] = false
}

func (g *Grid) Reset() {
	clear(g.data)
}

func SpawnSand(world *ecs.World, source *Source) ecs.Entity {
	e := world.NewEntity()
	ecs.Add(world, e, Position{source.X, source.Y})
	ecs.Add(world, e, Velocity{0, 2.0})
	ecs.Add(world, e, Falling{})
	return e
}

func DestroySand(world *ecs.World, source *Source, radius int) {
}

func ApplyPhysics(world *ecs.World, grid *Grid) {
	ents, ps, vs := ecs.QueryIntersect2[Position, Velocity](world)
	for i, e := range ents {
		_, isFalling := ecs.MutQuery[Falling](world, e)
		if isFalling {
			// GRAVITY
			v, _ := ecs.MutQuery[Velocity](world, e)
			v.Y += DELTA * GRAVITY

			// FALLING / COLLISION
			pos := ps[i]
			posYApprox := int(pos.Y)
			projPosY := pos.Y + DELTA*vs[i].Y
			for posYApprox < int(projPosY) {
				if grid.IsSet(int(pos.X), posYApprox+1) || posYApprox+1 == HEIGHT {
					ecs.Remove[Falling](world, e)
					v.Y = 0
					return
				}
				posYApprox++
			}
			p, _ := ecs.MutQuery[Position](world, e)
			grid.Clear(int(p.X), int(p.Y))
			p.Y = projPosY
			grid.Set(int(p.X), int(p.Y))
		}
	}
}

func Simulate(win *screen.Window, events <-chan any, shared *Shared) {
	world := ecs.NewWorld()
	InitializeWorld(&world)
	sandCount := 0
	source := Source{}
	gridLocal := NewGrid()
	worldTicker := time.NewTicker(SIMTICK)
	drawTicker := time.NewTicker(DRAWTICK)
	for {
		select {
		case event := <-events:
			switch e := event.(type) {
			case mouse.Event:
				if 0 < e.X && e.X < float32(WIDTH) {
					source.X = e.X
				}
				if 0 < e.Y && e.Y < float32(HEIGHT) {
					source.Y = e.Y
				}
				if e.Direction == mouse.DirPress {
					source.isActive = true
				} else if e.Direction == mouse.DirRelease {
					source.isActive = false
				}
			}
		default:
		}
		// Spawn Sand
		if source.isActive && sandCount < MAXSAND && !gridLocal.IsSet(int(source.X), int(source.Y)) {
			SpawnSand(&world, &source)
			sandCount++
			gridLocal.Set(int(source.X), int(source.Y))
		}
		// Simulate Physics
		ApplyPhysics(&world, &gridLocal)
		select {
		case <-drawTicker.C:
			shared.mu.Lock()
			copy(shared.grid.data, gridLocal.data)
			shared.mu.Unlock()
			(*win).Send(paint.Event{})
		default:
		}
		<-worldTicker.C // Block until update time has elapsed.
	}
}
