# Ollama Model Usage Guide

## Model Assignments

### **qwen2.5:14b** (Default Workhorse)
- General questions & research
- Swedish language tasks (Fitness Ledger UI, docs)
- Architecture decisions
- Quick coding questions
- Homelab documentation

### **deepseek-coder-v2:16b** (Coding Specialist)
- Complex refactoring (.NET, Go, React)
- Code reviews & debugging
- Algorithm implementation
- Database query optimization
- Docker/K8s configurations

### **llama3.2-vision:11b** (Vision Tasks)
- Screenshot analysis
- Food photos → recipes
- Diagram explanations
- Spanish text recognition (menus, signs)
- Error message screenshots

### **qwen2.5:7b** (Fast Iteration)
- Quick scripts & one-liners
- PowerShell/bash snippets
- Simple debugging
- When 14b is busy

### **aya-expanse:8b** (Language Tutor)
- Spanish exercises & drills
- Grammar explanations
- Vocab practice
- A1-B2 level content
- Swedish ↔ Spanish translation

### **nomic-embed-text** (Background Service)
- Semantic search on docs
- RAG for homelab wikis
- Code similarity search

## Concurrent Combinations
- **Vision + 7b**: Screenshot debugging
- **Aya + 7b**: Spanish practice while coding
- **14b solo**: Max quality for complex tasks

## TODO: Agent Setup
- [ ] Create coding agent (DeepSeek primary, Qwen fallback)
- [ ] Spanish tutor agent (Aya with exercise templates)
- [ ] Vision agent (auto-explain screenshots)
- [ ] Homelab docs agent (Qwen + nomic-embed RAG)
- [ ] Fitness app agent (Qwen for Swedish UI generation)
