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
	Tipo     string `json:"tipo"`
	Porta    int    `json:"porta"`
	Conteudo string `json:"conteudo"`
}

func (m *Mensagem) ParaBytes() ([]byte, error) {
	dados, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("erro ao serializar mensagem: %w", err)
	}
	return append(dados, '\n'), nil
}

func MensagemDosBytes(dados []byte) (*Mensagem, error) {
	var m Mensagem
	err := json.Unmarshal(dados, &m)
	if err != nil {
		return nil, fmt.Errorf("erro ao desserializar mensagem: %w", err)
	}
	return &m, nil
}
