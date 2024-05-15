package main

import (
	"image"
	"image/color"
	"log"
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
	PAGESIZE = 8
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
	ecs.Initialize[Position](world, PAGESIZE)
	ecs.Initialize[Velocity](world, PAGESIZE)
	ecs.Initialize[Falling](world, PAGESIZE)
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

func SpawnSand(world *ecs.World, source *Source, r int) {
	dx := (source.p.X - source.prev.X)
	dy := (source.p.Y - source.prev.Y)
	h := int(source.p.X)
	k := int(source.p.Y)
	for y := k - r; y < k+r; y++ {
		for x := h - r; x < h+r; x++ {
			if (x-h)*(x-h)+(y-k)*(y-k) <= r*r &&
				x >= 0 && y >= 0 && x < WIDTH && y < HEIGHT {
				e := world.NewEntity()
				source.v.X = dx/DELTA/2.0 + (rand.Float32()-rand.Float32())/DELTA/2.0
				source.v.Y = dy/DELTA/2.0 + (rand.Float32()-rand.Float32())/DELTA/2.0
				ecs.Add(world, e, Position{float32(x), float32(y)})
				ecs.Add(world, e, Velocity{source.v.X, source.v.Y})
				ecs.Add(world, e, Falling{})
			}
		}
	}
}

func DestroySand(world *ecs.World, source *Source, radius int) {
}

func ApplyPhysics(world *ecs.World, grid *Grid, col *Grid) {
	ents, _ := ecs.Query[Falling](world)
	for _, e := range ents {
		p, _ := ecs.GetMut[Position](world, e)
		v, _ := ecs.GetMut[Velocity](world, e)

		// GRAVITY
		v.Y = min(v.Y+DELTA*GRAVITY, MAXVEL)

		// MOTION
		pNextX := p.X + DELTA*v.X
		pNextY := p.Y + DELTA*v.Y

		// COLLISION
		colSet := false
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
			colSet = true
			ecs.RemoveAndClean[Falling](world, e)
		} else if col.IsSet(int(pNextX), int(pNextY)) {
			x := int(pNextX)
			y := int(pNextY)
			for {
				l := max(x-1, 0)
				r := min(x+1, WIDTH-1)
				setL := col.IsSet(l, y)
				setR := col.IsSet(r, y)
				if setL && setR {
					y = max(y-1, 0)
					if y == 0 {
						break
					}
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
				if !col.IsSet(x, y) {
					for (y+1) < HEIGHT && !col.IsSet(x, y+1) {
						y++
					}
					break
				}
			}
			for (y+1) < HEIGHT && !col.IsSet(x, y+1) {
				y++
			}

			col.Set(x, y)
			pNextX = float32(x)
			pNextY = float32(y)
			colSet = true
			ecs.RemoveAndClean[Falling](world, e)
		}

		if !colSet {
			grid.Clear(int(p.X), int(p.Y))
		}
		p.X = pNextX
		p.Y = pNextY
		grid.Set(int(p.X), int(p.Y))
	}
}

func Simulate(win *screen.Window, events <-chan any, shared *Shared) {
	world := ecs.NewWorld(ecs.WorldOptions{
		EntityLimit:    WIDTH * HEIGHT,
		RecycleLimit:   1024,
		ComponentLimit: 255,
	})
	InitializeWorld(&world)
	sandCount := 0
	source := Source{}
	gridLocal := NewGrid()
	collision := NewGrid()
	worldTicker := time.NewTicker(SIMTICK)
	drawTicker := time.NewTicker(DRAWTICK)
	profileTicker := time.NewTicker(time.Second)
	for {
		// Handle Events
		select {
		case event := <-events:
			switch e := event.(type) {
			case mouse.Event:
				source.prev.X = source.p.X
				source.prev.Y = source.p.Y
				source.p.X = max(min(e.X, WIDTH-1), 0)
				source.p.Y = max(min(e.Y, HEIGHT-1), 0)
				source.isActive = (source.isActive || (e.Direction == mouse.DirPress)) && (e.Direction != mouse.DirRelease)
			}
		default:
		}

		// Spawn Sand
		if source.isActive && !gridLocal.IsSet(int(source.p.X), int(source.p.Y)) && sandCount < MAXSAND {
			SpawnSand(&world, &source, 8)
			sandCount++
			//gridLocal.Set(int(source.p.X), int(source.p.Y))
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

		// Report Memory Usage
		select {
		case <-profileTicker.C:
			psize := ecs.MemUsage[Position](&world)
			vsize := ecs.MemUsage[Velocity](&world)
			fsize := ecs.MemUsage[Falling](&world)
			log.Printf("ENT:   %d", world.EntityCount())
			log.Printf("MEM:   [p,v,f] = [%d,%d,%d]", psize, vsize, fsize)
			log.Printf("TOTAL: %d", world.MemUsage()+psize+vsize+fsize)
			log.Println()
			ecs.Sweep[Falling](&world)
		default:
		}

		// Block until update time has elapsed.
		<-worldTicker.C
	}
}
