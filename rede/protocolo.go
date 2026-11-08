package rede

import (
	"encoding/json"
	"fmt"
)

const (
	TipoRequisicao = "REQ" 
	TipoResposta   = "RES" 
)

type Mensagem struct {
	Tipo         string `json:"tipo"`          
	IPOrigem     string `json:"ip_origem"`     
	PortaOrigem  int    `json:"porta_origem"`  
	IPDestino    string `json:"ip_destino"`    
	PortaDestino int    `json:"porta_destino"` 
	Conteudo     string `json:"conteudo"`      
}

// json -> Bytes. \n para marcar o fim
func (m *Mensagem) ParaBytes() ([]byte, error) {
	dados, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("erro ao serializar mensagem: %w", err)
	}
	return append(dados, '\n'), nil
}

// extrair json dos bytes brutos
func MensagemDosBytes(dados []byte) (*Mensagem, error) {
	var m Mensagem
	err := json.Unmarshal(dados, &m)
	if err != nil {
		return nil, fmt.Errorf("erro ao desserializar mensagem: %w", err)
	}
	return &m, nil
}
