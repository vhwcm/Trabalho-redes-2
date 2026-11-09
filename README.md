# Trabalho de Redes II


```mermaid
flowchart TD
    subgraph Clientes ["Clientes"]
        C1["Cliente TCP"]
    end

    subgraph LB ["Balanceador de Carga"]
        LoadBalancer["Load Balancer (:8080)
        Algoritmo: Least Connections"]
    end

    subgraph Monitoramento ["Monitoramento & Métricas"]
        Mon["Monitor / Tabela de Servidores (:9000)"]
    end

    subgraph Backends ["Cluster de Servidores Backend"]
        S1["Servidor 1 (:8081)"]
        S2["Servidor 2 (:8082)"]
        SN["Servidor N (:8080 + N)"]
    end

    %% Fluxo de Requisições de Dados
    C1 -->|"1. Conexão TCP (:8080)"| LoadBalancer
    LoadBalancer -->|"2. Repasse Least Connections (:8081)"| S1
    LoadBalancer -->|"2. Repasse Least Connections (:8082)"| S2
    LoadBalancer -->|"2. Repasse Least Connections"| SN

    %% Fluxo de Verificação de Conectividade TCP
    Mon -.->|"Checagem TCP Handshake (:8081)"| S1
    Mon -.->|"Checagem TCP Handshake (:8082)"| S2
    Mon -.->|"Checagem TCP Handshake"| SN

    %% Consulta de Métricas
    LoadBalancer -.->|"Consulta Servidores & Conexões (:9000)"| Mon
    C1 -.->|"Consulta Status Global (:9000)"| Mon
```

---

Balanceamento: Least Connections

---

## portas       

| Componente | Porta Padrão | Protocolo de Transporte | Formato do Payload | Descrição / Papel no Sistema |
| :--- | :--- | :--- | :--- | :--- |
| **Load Balancer** | `:8080` | TCP (`net.Listen`) | JSON (`Mensagem`) | Ponto único de entrada dos clientes. Aplica **Least Connections** para escolher o backend ativo. |
| **Servidores Backend** | `:8081` a `:8080 + N` | TCP (`net.Listen`) | JSON (`Mensagem`) | Processam as requisições dos clientes. |
| **Monitor (Métricas)** | `:9000` | TCP (`net.Listen`) | JSON (`Mensagem`) | Mantém a `TabelaServidores`, checa se as portas TCP estão abertas (UP/DOWN) e expõe o status atualizado do cluster. |
| **Clientes** | Portas Efêmeras do SO | TCP (`net.Dial`) | JSON (`Mensagem`) | Conectam-se ao Load Balancer (`:8080`) para solicitar tarefas ou ao Monitor (`:9000`) para ver métricas. |

---