package agent

// AgentRepository defines the interface for agent storage.
// Implemented by Repository (SQLite) and PostgresRepository.
type AgentRepository interface {
	InitSchema() error
	Create(agent *AgentRecord) error
	Update(agent *AgentRecord) error
	Get(id string) (*AgentRecord, error)
	List() ([]*AgentRecord, error)
	ListEnabled() ([]*AgentRecord, error)
	Delete(id string) error
}
