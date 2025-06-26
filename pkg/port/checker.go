package port

// Checker define la interfaz para una comprobación de salud individual.
// Cualquier componente que necesite ser verificado (BD, API externa, etc.)
// debe implementar esta interfaz.
type Checker interface {
	Check() error
}
