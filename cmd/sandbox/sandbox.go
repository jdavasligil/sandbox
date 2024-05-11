package main

import (
	"image"
	"image/color"
	"log"
	"math"
	"math/rand"
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
	WIDTH    = 800
	HEIGHT   = 800
	SIMRATE  = 64
	FRMRATE  = 60
	DELTA    = 1.0 / SIMRATE
	MAXVEL   = 4.0 * SIMRATE
	SIMTICK  = time.Second / SIMRATE
	DRAWTICK = time.Second / FRMRATE
	MAXSAND  = WIDTH * HEIGHT / 2
	GRAVITY  = 490.0 // px/s/s
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
	prev Position
	p    Position
	v    Velocity

	isActive bool
}

type Grid struct {
	sync.Mutex
	data []bool
}

func NewGrid() Grid {
	return Grid{
		data: make([]bool, WIDTH*HEIGHT),
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
	source.v.X = (source.p.X-source.prev.X)/DELTA/2.0 + (rand.Float32()-rand.Float32())/DELTA/2.0
	source.v.Y = (source.p.Y-source.prev.Y)/DELTA/2.0 + (rand.Float32()-rand.Float32())/DELTA/2.0
	e := world.NewEntity()
	ecs.Add(world, e, Position{source.p.X, source.p.Y})
	ecs.Add(world, e, Velocity{source.v.X, source.v.Y})
	ecs.Add(world, e, Falling{})
	return e
}

func DestroySand(world *ecs.World, source *Source, radius int) {
}

func ApplyPhysics(world *ecs.World, grid *Grid, col *Grid) {
	ents, _ := ecs.QueryAll[Falling](world)
	for _, e := range ents {
		p, _ := ecs.MutQuery[Position](world, e)
		v, _ := ecs.MutQuery[Velocity](world, e)

		// GRAVITY
		v.Y = float32(math.Min(float64(v.Y)+DELTA*GRAVITY, MAXVEL))

		// MOTION
		pNextX := p.X + DELTA*v.X
		pNextY := p.Y + DELTA*v.Y

		// COLLISION
		if pNextX < 0 {
			v.X = -v.X
			pNextX = 0
		} else if pNextX >= WIDTH {
			v.X = -v.X
			pNextX = WIDTH - 1
		}
		if pNextY < 0 {
			v.Y = -v.Y
			pNextY = 0
		} else if pNextY >= HEIGHT {
			v.X = 0
			v.Y = 0
			pNextY = HEIGHT - 1
			for col.IsSet(int(pNextX), int(pNextY)) {
				pNextY -= 1
			}
			col.Set(int(pNextX), int(pNextY))
			ecs.Remove[Falling](world, e)
		} else if col.IsSet(int(pNextX), int(pNextY)) {
			l := int(math.Max(float64(pNextX)-1, 0))
			r := int(math.Min(float64(pNextX)+1, WIDTH-1))
			x := int(pNextX)
			y := int(pNextY)
			setL := col.IsSet(l, int(pNextY))
			setR := col.IsSet(r, int(pNextY))
			if setL && setR {
				y--
			} else if !(setL || setR) {
				if l%2 == 0 {
					x = l
				} else {
					x = r
				}
			} else if setL {
				x = r
			} else {
				x = l
			}

			col.Set(x, y)
			pNextX = float32(x)
			pNextY = float32(y)
			ecs.Remove[Falling](world, e)
		}

		grid.Clear(int(p.X), int(p.Y))
		p.X = pNextX
		p.Y = pNextY
		grid.Set(int(p.X), int(p.Y))
	}
}

func Simulate(win *screen.Window, events <-chan any, shared *Shared) {
	world := ecs.NewWorld()
	InitializeWorld(&world)
	sandCount := 0
	source := Source{}
	gridLocal := NewGrid()
	collision := NewGrid()
	worldTicker := time.NewTicker(SIMTICK)
	drawTicker := time.NewTicker(DRAWTICK)
	for {
		// Handle Events
		select {
		case event := <-events:
			switch e := event.(type) {
			case mouse.Event:
				source.prev.X = source.p.X
				source.prev.Y = source.p.Y
				source.p.X = float32(math.Max(math.Min(float64(e.X), WIDTH), 0))
				source.p.Y = float32(math.Max(math.Min(float64(e.Y), HEIGHT), 0))
				source.isActive = (source.isActive || (e.Direction == mouse.DirPress)) && (e.Direction != mouse.DirRelease)
			}
		default:
		}

		// Spawn Sand
		if source.isActive && !gridLocal.IsSet(int(source.p.X), int(source.p.X)) && sandCount < MAXSAND {
			SpawnSand(&world, &source)
			sandCount++
			gridLocal.Set(int(source.p.X), int(source.p.Y))
		}

		// Simulate Physics
		ApplyPhysics(&world, &gridLocal, &collision)

		// Draw Call
		select {
		case <-drawTicker.C:
			shared.mu.Lock()
			copy(shared.grid.data, gridLocal.data)
			shared.mu.Unlock()
			(*win).Send(paint.Event{})
		default:
		}

		// Block until update time has elapsed.
		<-worldTicker.C
	}
}
