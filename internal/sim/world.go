package sim

type EntityID uint64
type RegionID uint32
type Tick uint64

type Position struct {
    X int32 `json:"x"`
    Y int32 `json:"y"`
}

type WorldConfig struct {
    Width              int32  `json:"width"`
    Height             int32  `json:"height"`
    RegionWidth        int32  `json:"region_width"`
    RegionHeight       int32  `json:"region_height"`
    InitialPlants      int    `json:"initial_plants"`
    InitialHerbivores  int    `json:"initial_herbivores"`
    InitialPredators   int    `json:"initial_predators"`
    Seed               uint64 `json:"seed"`
}

type World struct {
    ID         string
    Tick       Tick
    Config     WorldConfig
    Entities   map[EntityID]Entity
    NextEntity EntityID
}